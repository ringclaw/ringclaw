package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ringclaw/ringclaw/config"
	"github.com/ringclaw/ringclaw/messaging"
)

type runtimeMeshTaskClient struct {
	controlPlaneURL string
	botID           string
	bootstrapToken  string
}

type runtimeMeshTasksRequest struct {
	BotID          string `json:"bot_id"`
	BootstrapToken string `json:"bootstrap_token"`
	Limit          int    `json:"limit,omitempty"`
}

type runtimeMeshTasksResult struct {
	Tasks []messaging.MeshRuntimeTask `json:"tasks"`
}

type runtimeMeshTaskRespondRequest struct {
	BotID          string                                 `json:"bot_id"`
	BootstrapToken string                                 `json:"bootstrap_token"`
	Status         string                                 `json:"status"`
	Result         string                                 `json:"result,omitempty"`
	ActionEvents   []messaging.MeshRuntimeTaskActionEvent `json:"action_events,omitempty"`
	Details        map[string]interface{}                 `json:"details,omitempty"`
}

func (c runtimeMeshTaskClient) PollMeshTasks(ctx context.Context, req messaging.MeshRuntimeTaskPollRequest) ([]messaging.MeshRuntimeTask, error) {
	var result runtimeMeshTasksResult
	err := postJSON(ctx, c.controlPlaneURL, "/runtime/v1/mesh/tasks", runtimeMeshTasksRequest{
		BotID:          c.botID,
		BootstrapToken: c.bootstrapToken,
		Limit:          req.Limit,
	}, http.StatusOK, &result)
	return result.Tasks, err
}

func (c runtimeMeshTaskClient) RespondMeshTask(ctx context.Context, taskID string, resp messaging.MeshRuntimeTaskResponse) error {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return fmt.Errorf("mesh task id is required")
	}
	return postJSON(ctx, c.controlPlaneURL, "/runtime/v1/mesh/tasks/"+taskID+"/respond", runtimeMeshTaskRespondRequest{
		BotID:          c.botID,
		BootstrapToken: c.bootstrapToken,
		Status:         resp.Status,
		Result:         resp.Result,
		ActionEvents:   resp.ActionEvents,
		Details:        resp.Details,
	}, http.StatusOK, nil)
}

func startRuntimeMeshPoller(ctx context.Context, cfg *config.Config, c *clients, handler *messaging.Handler) func() {
	if cfg == nil || !cfg.Mesh.Enabled {
		return func() {}
	}
	controlPlaneURL := firstNonEmptyString(cfg.Mesh.ControlPlaneURL, firstEnv("AVA_CONTROL_PLANE_URL", "CONTROL_PLANE_URL"))
	botID := firstNonEmptyString(cfg.Bot.ID, firstEnv("RINGCLAW_BOT_ID", "BOT_ID"))
	bootstrapToken := firstEnv("RINGCLAW_BOOTSTRAP_TOKEN", "BOOTSTRAP_TOKEN")
	if strings.TrimSpace(controlPlaneURL) == "" || strings.TrimSpace(botID) == "" || strings.TrimSpace(bootstrapToken) == "" {
		slog.Warn("mesh poller disabled: missing control plane URL, bot ID, or bootstrap token", "component", "runtime_mesh")
		return func() {}
	}
	if c == nil || c.bot == nil || handler == nil {
		return func() {}
	}
	interval := 10 * time.Second
	if cfg.Mesh.PollInterval != "" {
		parsed, err := time.ParseDuration(cfg.Mesh.PollInterval)
		if err != nil {
			slog.Warn("invalid mesh poll interval; using default", "component", "runtime_mesh", "value", cfg.Mesh.PollInterval, "error", err)
		} else if parsed > 0 {
			interval = parsed
		}
	}
	meshClient := runtimeMeshTaskClient{
		controlPlaneURL: controlPlaneURL,
		botID:           botID,
		bootstrapToken:  bootstrapToken,
	}
	pollCtx, cancel := context.WithCancel(ctx)
	go runRuntimeMeshPoller(pollCtx, interval, cfg, c, handler, meshClient)
	return cancel
}

func runRuntimeMeshPoller(ctx context.Context, interval time.Duration, cfg *config.Config, c *clients, handler *messaging.Handler, meshClient runtimeMeshTaskClient) {
	slog.Info("runtime mesh poller started", "component", "runtime_mesh", "agentID", cfg.Mesh.AgentID, "roleID", cfg.Mesh.RoleID)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		ag := handler.GetDefaultAgent()
		if ag != nil {
			runner := messaging.NewMeshRunner(messaging.MeshRunnerOptions{
				Client:         meshClient,
				Agent:          ag,
				ReplyClient:    c.bot,
				ActionClient:   c.lookupClient(),
				DefaultChatID:  firstConfiguredChatID(cfg),
				Capabilities:   cfg.RC.Capabilities,
				AllowedActions: cfg.Mesh.AllowedActions,
			})
			if err := runner.ProcessOnce(ctx); err != nil {
				slog.Warn("runtime mesh poll failed", "component", "runtime_mesh", "error", err)
			}
		}
		select {
		case <-ctx.Done():
			slog.Info("runtime mesh poller stopped", "component", "runtime_mesh")
			return
		case <-ticker.C:
		}
	}
}

func firstConfiguredChatID(cfg *config.Config) string {
	if cfg == nil || len(cfg.RC.ChatIDs) == 0 {
		return ""
	}
	return strings.TrimSpace(cfg.RC.ChatIDs[0])
}
