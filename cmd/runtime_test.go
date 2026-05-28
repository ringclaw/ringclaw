package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ringclaw/ringclaw/config"
)

func TestRuntimeOptionsApplyEnv(t *testing.T) {
	t.Setenv("CONTROL_PLANE_URL", "https://ava-control-plane.example")
	t.Setenv("BOT_ID", "personal-ava-user-1")
	t.Setenv("BOOTSTRAP_TOKEN", "bootstrap-token")
	t.Setenv("POD_NAME", "pod-a")

	opts := runtimeStartOptions{}
	opts.applyEnv()

	if opts.ControlPlaneURL != "https://ava-control-plane.example" {
		t.Fatalf("ControlPlaneURL = %q", opts.ControlPlaneURL)
	}
	if opts.BotID != "personal-ava-user-1" || opts.BootstrapToken != "bootstrap-token" || opts.PodName != "pod-a" {
		t.Fatalf("applyEnv() did not populate runtime identity: %#v", opts)
	}
}

func TestClaimRuntimeConfigPostsBootstrapTokenAndDecodesConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/runtime/v1/claim" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		var req runtimeClaimRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.BotID != "personal-ava-user-1" || req.PodName != "pod-a" || req.BootstrapToken != "bootstrap-token" {
			t.Fatalf("claim request = %#v", req)
		}
		_ = json.NewEncoder(w).Encode(runtimeClaimResult{
			Config: config.Config{
				Bot: config.BotConfig{
					ID:                    "personal-ava-user-1",
					TenantID:              "account-1",
					OwnerUserID:           "user-1",
					ConversationNamespace: "account-1/personal-ava-user-1",
				},
				DefaultAgent: "codex",
				Agents: map[string]config.AgentConfig{
					"codex": {Type: "http", Endpoint: "https://agent.example", APIKey: "agent-token"},
				},
				RC: config.RCConfig{
					BotToken:      "rc-bot-token",
					ChatIDs:       []string{"chat-1"},
					SourceUserIDs: []string{"user-1"},
					Capabilities:  []string{"video", "phone"},
				},
			},
		})
	}))
	defer server.Close()

	cfg, err := claimRuntimeConfig(context.Background(), server.URL, runtimeClaimRequest{
		BotID:          "personal-ava-user-1",
		PodName:        "pod-a",
		BootstrapToken: "bootstrap-token",
	})
	if err != nil {
		t.Fatalf("claimRuntimeConfig() error = %v", err)
	}
	if cfg.Bot.ID != "personal-ava-user-1" || cfg.RC.BotToken != "rc-bot-token" {
		t.Fatalf("claimed config = %#v", cfg)
	}
	if strings.Join(cfg.RC.Capabilities, ",") != "video,phone" {
		t.Fatalf("capabilities = %#v", cfg.RC.Capabilities)
	}
}

func TestSendRuntimeHeartbeatUsesBootstrapToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/runtime/v1/heartbeat" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		var req runtimeHeartbeatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode heartbeat: %v", err)
		}
		if req.BootstrapToken != "bootstrap-token" || req.Status != runtimeStatusHealthy {
			t.Fatalf("heartbeat request = %#v", req)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	err := sendRuntimeHeartbeat(context.Background(), server.URL, runtimeHeartbeatRequest{
		BotID:          "personal-ava-user-1",
		PodName:        "pod-a",
		BootstrapToken: "bootstrap-token",
		Status:         runtimeStatusHealthy,
		Capabilities:   []string{"message", "video"},
	})
	if err != nil {
		t.Fatalf("sendRuntimeHeartbeat() error = %v", err)
	}
}

func TestWriteClaimedRuntimeConfigPersistsConfigForStartup(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "runtime.json")
	cfg := &config.Config{
		Bot:          config.BotConfig{ID: "personal-ava-user-1"},
		DefaultAgent: "codex",
		Agents: map[string]config.AgentConfig{
			"codex": {Type: "http", Endpoint: "https://agent.example", APIKey: "agent-token"},
		},
		RC: config.RCConfig{
			BotToken:      "rc-bot-token",
			ChatIDs:       []string{"chat-1"},
			SourceUserIDs: []string{"user-1"},
			Capabilities:  []string{"video", "phone"},
		},
	}

	path, err := writeClaimedRuntimeConfig(cfgPath, cfg)
	if err != nil {
		t.Fatalf("writeClaimedRuntimeConfig() error = %v", err)
	}
	if path != cfgPath {
		t.Fatalf("path = %q, want %q", path, cfgPath)
	}
	t.Setenv("RINGCLAW_CONFIG", path)
	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Bot.ID != "personal-ava-user-1" || loaded.RC.BotToken != "rc-bot-token" {
		t.Fatalf("loaded config = %#v", loaded)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %v, want 0600", got)
	}
}
