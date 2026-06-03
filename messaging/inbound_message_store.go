package messaging

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ringclaw/ringclaw/ringcentral"
)

var (
	inboundConfirmPattern = regexp.MustCompile(`(?i)\bconfirm\b`)
	inboundOrderPattern   = regexp.MustCompile(`(?i)#?\b([A-Z]\d{3,})\b`)
)

var inboundComplaintSignals = []string{
	"complaint",
	"worst",
	"lawsuit",
	"didn't show up",
	"didnt show up",
	"no-show",
	"no show",
	"late again",
	"terrible",
	"awful",
	"unacceptable",
	"lowe's",
	"lowes",
}

type InboundMessageStoreConfig struct {
	AlertChatID  string
	RouteChatID  string
	Capabilities []string
}

type InboundMessageStoreProcessor struct {
	alertChatID string
	routeChatID string
	smsEnabled  bool

	mu   sync.Mutex
	seen map[string]time.Time
}

func NewInboundMessageStoreProcessor(cfg InboundMessageStoreConfig) *InboundMessageStoreProcessor {
	return &InboundMessageStoreProcessor{
		alertChatID: strings.TrimSpace(cfg.AlertChatID),
		routeChatID: strings.TrimSpace(cfg.RouteChatID),
		smsEnabled:  hasInboundCapability(cfg.Capabilities, "sms"),
		seen:        make(map[string]time.Time),
	}
}

func (p *InboundMessageStoreProcessor) HandleRecord(ctx context.Context, client *ringcentral.Client, record ringcentral.MessageStoreItem) {
	if p == nil || client == nil {
		return
	}
	if p.isDuplicate(record) {
		return
	}
	msgType := strings.ToLower(strings.TrimSpace(record.Type))
	switch msgType {
	case "sms", "mms", "pager":
		p.handleInboundSMS(ctx, client, record)
	case "fax":
		p.handleInboundFax(ctx, client, record)
	default:
		slog.Info("ignoring unsupported inbound message-store type",
			"component", "inbound",
			"type", record.Type,
			"id", fmt.Sprint(record.ID),
		)
	}
}

func (p *InboundMessageStoreProcessor) isDuplicate(record ringcentral.MessageStoreItem) bool {
	key := fmt.Sprintf("%v|%s|%s|%s", record.ID, record.Type, record.LastModifiedTime, record.MessageStatus)
	now := time.Now()

	p.mu.Lock()
	defer p.mu.Unlock()

	for k, seenAt := range p.seen {
		if now.Sub(seenAt) > 30*time.Minute {
			delete(p.seen, k)
		}
	}
	if _, ok := p.seen[key]; ok {
		return true
	}
	p.seen[key] = now
	return false
}

func (p *InboundMessageStoreProcessor) handleInboundSMS(ctx context.Context, client *ringcentral.Client, record ringcentral.MessageStoreItem) {
	body := strings.TrimSpace(record.Subject)
	if body == "" {
		slog.Info("inbound sms without body",
			"component", "inbound",
			"id", fmt.Sprint(record.ID),
			"from", record.From.PhoneNumber,
		)
		return
	}

	senderLabel := inboundSenderLabel(record.From)
	orderID := inboundOrderID(body)
	receivedAt := inboundReceivedAt(record)

	if inboundConfirmPattern.MatchString(body) {
		recordAgentActionEvent(ctx, ActionEvent{
			Type:   "INBOUND_SMS",
			Status: "confirm_received",
			Details: map[string]any{
				"from":     record.From.PhoneNumber,
				"order_id": orderID,
			},
		})
		p.postAlert(ctx, client, formatInboundConfirmNotice(senderLabel, orderID, body, receivedAt))
		return
	}

	if !isComplaintSignal(body) {
		slog.Info("inbound sms received",
			"component", "inbound",
			"from", record.From.PhoneNumber,
			"id", fmt.Sprint(record.ID),
		)
		return
	}

	replyStatus := "未发送"
	if p.smsEnabled && strings.TrimSpace(record.From.PhoneNumber) != "" {
		if _, err := client.SendSMS(ctx, &ringcentral.CreateSMSRequest{
			To: []ringcentral.PhoneNumberRef{
				{PhoneNumber: strings.TrimSpace(record.From.PhoneNumber)},
			},
			Text: buildComplaintReply(orderID),
		}); err != nil {
			slog.Warn("auto-reply sms failed",
				"component", "inbound",
				"from", record.From.PhoneNumber,
				"error", err,
			)
			replyStatus = "发送失败"
		} else {
			replyStatus = "已发"
		}
	}

	taskID := p.createComplaintTask(ctx, client, senderLabel, orderID, body)
	p.postAlert(ctx, client, formatInboundComplaintNotice(senderLabel, orderID, body, replyStatus, taskID))
	p.postRoute(ctx, client, formatComplaintRoute(orderID, complaintSummary(body), taskID))

	recordAgentActionEvent(ctx, ActionEvent{
		Type:   "INBOUND_SMS",
		Status: "complaint_escalated",
		Details: map[string]any{
			"from":        record.From.PhoneNumber,
			"order_id":    orderID,
			"reply_status": replyStatus,
			"task_id":     taskID,
		},
	})
}

func (p *InboundMessageStoreProcessor) handleInboundFax(ctx context.Context, client *ringcentral.Client, record ringcentral.MessageStoreItem) {
	senderLabel := inboundSenderLabel(record.From)
	p.postAlert(ctx, client, formatInboundFaxNotice(senderLabel, record.Subject, len(record.Attachments)))
	recordAgentActionEvent(ctx, ActionEvent{
		Type:   "INBOUND_FAX",
		Status: "received",
		Details: map[string]any{
			"from":        record.From.PhoneNumber,
			"attachments": len(record.Attachments),
		},
	})
}

func (p *InboundMessageStoreProcessor) createComplaintTask(ctx context.Context, client *ringcentral.Client, senderLabel, orderID, complaint string) string {
	if strings.TrimSpace(p.alertChatID) == "" {
		return ""
	}
	subject := "URGENT inbound complaint"
	if orderID != "" {
		subject += " #" + orderID
	}
	task, err := client.CreateTask(ctx, p.alertChatID, &ringcentral.CreateTaskRequest{
		Subject:     subject,
		Description: fmt.Sprintf("%s\n%s", senderLabel, strings.TrimSpace(complaint)),
		DueDate:     time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339),
		Color:       "Red",
	})
	if err != nil {
		slog.Warn("failed to create inbound complaint task",
			"component", "inbound",
			"chatID", p.alertChatID,
			"error", err,
		)
		return ""
	}
	return task.ID
}

func (p *InboundMessageStoreProcessor) postAlert(ctx context.Context, client *ringcentral.Client, text string) {
	if strings.TrimSpace(p.alertChatID) == "" || strings.TrimSpace(text) == "" {
		return
	}
	logSendError(SendTextReply(ctx, client, p.alertChatID, text))
}

func (p *InboundMessageStoreProcessor) postRoute(ctx context.Context, client *ringcentral.Client, text string) {
	target := p.routeChatID
	if target == "" {
		target = p.alertChatID
	}
	if strings.TrimSpace(target) == "" || strings.TrimSpace(text) == "" {
		return
	}
	logSendError(SendTextReply(ctx, client, target, text))
}

func hasInboundCapability(capabilities []string, capability string) bool {
	capability = strings.ToLower(strings.TrimSpace(capability))
	if len(capabilities) == 0 {
		return true
	}
	for _, entry := range capabilities {
		if strings.ToLower(strings.TrimSpace(entry)) == capability {
			return true
		}
	}
	return false
}

func inboundSenderLabel(from ringcentral.SMSParty) string {
	if name := strings.TrimSpace(from.Name); name != "" && strings.TrimSpace(from.PhoneNumber) != "" {
		return name + " " + strings.TrimSpace(from.PhoneNumber)
	}
	if name := strings.TrimSpace(from.Name); name != "" {
		return name
	}
	if phone := strings.TrimSpace(from.PhoneNumber); phone != "" {
		return phone
	}
	return "unknown sender"
}

func inboundOrderID(text string) string {
	matches := inboundOrderPattern.FindStringSubmatch(strings.TrimSpace(text))
	if len(matches) < 2 {
		return ""
	}
	return strings.ToUpper(strings.TrimSpace(matches[1]))
}

func inboundReceivedAt(record ringcentral.MessageStoreItem) string {
	raw := strings.TrimSpace(record.CreationTime)
	if raw == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return raw
	}
	return t.Local().Format("15:04")
}

func isComplaintSignal(text string) bool {
	normalized := strings.ToLower(strings.TrimSpace(text))
	if normalized == "" {
		return false
	}
	for _, signal := range inboundComplaintSignals {
		if strings.Contains(normalized, signal) {
			return true
		}
	}
	return strings.Count(normalized, "!") >= 3
}

func complaintSummary(text string) string {
	normalized := strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	return normalized
}

func buildComplaintReply(orderID string) string {
	if orderID != "" {
		return fmt.Sprintf("Hi, this is Keller. I'm so sorry about #%s. I'm escalating this to our manager right now and you will get a callback within 15 minutes.", orderID)
	}
	return "Hi, this is Keller. I'm so sorry about this. I'm escalating it to our manager right now and you will get a callback within 15 minutes."
}
