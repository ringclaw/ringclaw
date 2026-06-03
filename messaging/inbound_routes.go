package messaging

import (
	"fmt"
	"strings"

	"github.com/ringclaw/ringclaw/internal/util"
)

func formatInboundConfirmNotice(senderLabel, orderID, rawText, receivedAt string) string {
	line := fmt.Sprintf("✅ Inbound CONFIRM · %s", senderLabel)
	if orderID != "" {
		line += " · #" + orderID
	}
	if receivedAt != "" {
		line += " · " + receivedAt
	}
	if strings.TrimSpace(rawText) == "" {
		return line
	}
	return line + "\n" + util.Truncate(strings.TrimSpace(rawText), 160)
}

func formatInboundComplaintNotice(senderLabel, orderID, complaint, replyStatus, taskID string) string {
	lines := []string{
		fmt.Sprintf("⚠️ 投诉升级 · %s", senderLabel),
	}
	if orderID != "" {
		lines = append(lines, "订单：#"+orderID)
	}
	if complaint != "" {
		lines = append(lines, fmt.Sprintf("投诉：%s", util.Truncate(strings.TrimSpace(complaint), 220)))
	}
	if replyStatus != "" {
		lines = append(lines, "自动安抚："+replyStatus)
	}
	if taskID != "" {
		lines = append(lines, "Task："+taskID)
	}
	return strings.Join(lines, "\n")
}

func formatComplaintRoute(orderID, summary, taskID string) string {
	lines := []string{"[AGENT_ROUTE:COMPLAINT]"}
	if orderID != "" {
		lines = append(lines, "订单：#"+orderID)
	}
	if summary != "" {
		lines = append(lines, "投诉："+util.Truncate(strings.TrimSpace(summary), 120))
	}
	if taskID != "" {
		lines = append(lines, "Task："+taskID)
	}
	lines = append(lines, "@tom-bot 请调查派工情况")
	return strings.Join(lines, "\n")
}

func formatInboundFaxNotice(senderLabel, subject string, attachments int) string {
	lines := []string{
		fmt.Sprintf("📠 Inbound fax · %s", senderLabel),
	}
	if strings.TrimSpace(subject) != "" {
		lines = append(lines, "主题："+strings.TrimSpace(subject))
	}
	if attachments > 0 {
		lines = append(lines, fmt.Sprintf("附件：%d", attachments))
	}
	lines = append(lines, "传真已收到，后续可接 PDF 解析与台账处理。")
	return strings.Join(lines, "\n")
}
