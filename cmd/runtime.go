package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ringclaw/ringclaw/config"
	"github.com/spf13/cobra"
)

const (
	runtimeStatusHealthy  = "healthy"
	runtimeStatusDegraded = "degraded"
)

type runtimeStartOptions struct {
	ControlPlaneURL   string
	BotID             string
	BootstrapToken    string
	PodName           string
	ConfigOut         string
	HeartbeatInterval time.Duration
}

type runtimeClaimRequest struct {
	BotID          string `json:"bot_id"`
	PodName        string `json:"pod_name"`
	BootstrapToken string `json:"bootstrap_token"`
}

type runtimeClaimResult struct {
	Config config.Config `json:"config"`
}

type runtimeHeartbeatRequest struct {
	BotID          string   `json:"bot_id"`
	PodName        string   `json:"pod_name"`
	BootstrapToken string   `json:"bootstrap_token"`
	Status         string   `json:"status"`
	Capabilities   []string `json:"capabilities,omitempty"`
	LastError      string   `json:"last_error,omitempty"`
}

var runtimeOpts runtimeStartOptions

var runtimeCmd = &cobra.Command{
	Use:   "runtime",
	Short: "Run RingClaw as a managed bot runtime",
}

var runtimeStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Claim config from AVA Control Plane and start the bot runtime",
	RunE:  runRuntimeStart,
}

func init() {
	runtimeStartCmd.Flags().StringVar(&runtimeOpts.ControlPlaneURL, "control-plane", "", "AVA Control Plane base URL")
	runtimeStartCmd.Flags().StringVar(&runtimeOpts.BotID, "bot-id", "", "Bot ID to claim")
	runtimeStartCmd.Flags().StringVar(&runtimeOpts.BootstrapToken, "bootstrap-token", "", "Bootstrap token issued by AVA Control Plane")
	runtimeStartCmd.Flags().StringVar(&runtimeOpts.PodName, "pod-name", "", "Runtime pod name")
	runtimeStartCmd.Flags().StringVar(&runtimeOpts.ConfigOut, "config-out", "", "Write claimed config to this path before startup")
	runtimeStartCmd.Flags().DurationVar(&runtimeOpts.HeartbeatInterval, "heartbeat-interval", 30*time.Second, "Runtime heartbeat interval")
	runtimeCmd.AddCommand(runtimeStartCmd)
	rootCmd.AddCommand(runtimeCmd)
}

func (o *runtimeStartOptions) applyEnv() {
	if o.ControlPlaneURL == "" {
		o.ControlPlaneURL = firstEnv("AVA_CONTROL_PLANE_URL", "CONTROL_PLANE_URL")
	}
	if o.BotID == "" {
		o.BotID = firstEnv("RINGCLAW_BOT_ID", "BOT_ID")
	}
	if o.BootstrapToken == "" {
		o.BootstrapToken = firstEnv("RINGCLAW_BOOTSTRAP_TOKEN", "BOOTSTRAP_TOKEN")
	}
	if o.PodName == "" {
		o.PodName = firstEnv("POD_NAME", "HOSTNAME")
	}
}

func runRuntimeStart(cmd *cobra.Command, args []string) error {
	opts := runtimeOpts
	opts.applyEnv()
	if err := validateRuntimeStartOptions(opts); err != nil {
		return err
	}

	ctx, cancel := notifyContext(context.Background())
	defer cancel()

	cfg, err := claimRuntimeConfig(ctx, opts.ControlPlaneURL, runtimeClaimRequest{
		BotID:          opts.BotID,
		PodName:        opts.PodName,
		BootstrapToken: opts.BootstrapToken,
	})
	if err != nil {
		return err
	}

	configPath, err := writeClaimedRuntimeConfig(opts.ConfigOut, cfg)
	if err != nil {
		return err
	}
	if err := os.Setenv("RINGCLAW_CONFIG", configPath); err != nil {
		return fmt.Errorf("set RINGCLAW_CONFIG: %w", err)
	}

	stopHeartbeat := startRuntimeHeartbeat(ctx, opts, cfg)
	defer stopHeartbeat()

	foregroundFlag = true
	if err := runStart(cmd, args); err != nil {
		_ = sendRuntimeHeartbeat(ctx, opts.ControlPlaneURL, runtimeHeartbeatRequest{
			BotID:          opts.BotID,
			PodName:        opts.PodName,
			BootstrapToken: opts.BootstrapToken,
			Status:         runtimeStatusDegraded,
			Capabilities:   runtimeHeartbeatCapabilities(cfg),
			LastError:      err.Error(),
		})
		return err
	}
	return nil
}

func validateRuntimeStartOptions(opts runtimeStartOptions) error {
	if strings.TrimSpace(opts.ControlPlaneURL) == "" {
		return fmt.Errorf("control plane URL is required")
	}
	if strings.TrimSpace(opts.BotID) == "" {
		return fmt.Errorf("bot ID is required")
	}
	if strings.TrimSpace(opts.BootstrapToken) == "" {
		return fmt.Errorf("bootstrap token is required")
	}
	if strings.TrimSpace(opts.PodName) == "" {
		return fmt.Errorf("pod name is required")
	}
	return nil
}

func claimRuntimeConfig(ctx context.Context, controlPlaneURL string, req runtimeClaimRequest) (*config.Config, error) {
	var result runtimeClaimResult
	if err := postJSON(ctx, controlPlaneURL, "/runtime/v1/claim", req, http.StatusOK, &result); err != nil {
		return nil, fmt.Errorf("claim runtime config: %w", err)
	}
	return &result.Config, nil
}

func sendRuntimeHeartbeat(ctx context.Context, controlPlaneURL string, req runtimeHeartbeatRequest) error {
	if err := postJSON(ctx, controlPlaneURL, "/runtime/v1/heartbeat", req, http.StatusNoContent, nil); err != nil {
		return fmt.Errorf("send runtime heartbeat: %w", err)
	}
	return nil
}

func writeClaimedRuntimeConfig(path string, cfg *config.Config) (string, error) {
	if strings.TrimSpace(path) == "" {
		dir, err := os.MkdirTemp("", "ringclaw-runtime-*")
		if err != nil {
			return "", fmt.Errorf("create runtime config dir: %w", err)
		}
		path = filepath.Join(dir, "config.json")
	}
	if err := config.SaveTo(path, cfg); err != nil {
		return "", err
	}
	return path, nil
}

func startRuntimeHeartbeat(ctx context.Context, opts runtimeStartOptions, cfg *config.Config) func() {
	heartbeatCtx, cancel := context.WithCancel(ctx)
	interval := opts.HeartbeatInterval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	send := func(status, lastError string) {
		_ = sendRuntimeHeartbeat(heartbeatCtx, opts.ControlPlaneURL, runtimeHeartbeatRequest{
			BotID:          opts.BotID,
			PodName:        opts.PodName,
			BootstrapToken: opts.BootstrapToken,
			Status:         status,
			Capabilities:   runtimeHeartbeatCapabilities(cfg),
			LastError:      lastError,
		})
	}
	send(runtimeStatusHealthy, "")
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ticker.C:
				send(runtimeStatusHealthy, "")
			}
		}
	}()
	return cancel
}

func runtimeHeartbeatCapabilities(cfg *config.Config) []string {
	out := []string{"message"}
	for _, capability := range cfg.RC.Capabilities {
		if strings.TrimSpace(capability) != "" && !containsRuntimeString(out, capability) {
			out = append(out, capability)
		}
	}
	return out
}

func postJSON(ctx context.Context, baseURL, path string, in any, wantStatus int, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	url := strings.TrimRight(baseURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func firstEnv(names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}

func containsRuntimeString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
