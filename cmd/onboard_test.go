package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ringclaw/ringclaw/config"
)

func TestMergeStrings_TrimsSplitsAndDeduplicates(t *testing.T) {
	got := mergeStrings(
		[]string{"123", "alice@example.com"},
		[]string{" 456, 123 ", "ALICE@example.com", "789"},
	)
	want := []string{"123", "alice@example.com", "456", "789"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("mergeStrings() = %#v, want %#v", got, want)
	}
}

func TestValidatePrivateCredentialSet_RequiresAllFields(t *testing.T) {
	if err := validatePrivateCredentialSet("id", "secret", "jwt"); err != nil {
		t.Fatalf("validatePrivateCredentialSet() unexpected error = %v", err)
	}
	if err := validatePrivateCredentialSet("", "", ""); err != nil {
		t.Fatalf("validatePrivateCredentialSet() empty set error = %v", err)
	}
	if err := validatePrivateCredentialSet("id", "", "jwt"); err == nil {
		t.Fatal("validatePrivateCredentialSet() expected partial credential error")
	}
}

func TestOnboardOptionsApplyEnv(t *testing.T) {
	t.Setenv("RINGCLAW_BOT_ID", "bot-a")
	t.Setenv("RINGCLAW_TENANT_ID", "tenant-1")
	t.Setenv("RINGCLAW_OWNER_USER_ID", "owner-1")
	t.Setenv("RINGCLAW_CONVERSATION_NAMESPACE", "tenant-1/bot-a")
	t.Setenv("RC_BOT_TOKEN", "bot")
	t.Setenv("RC_CLIENT_ID", "cid")
	t.Setenv("RC_CLIENT_SECRET", "secret")
	t.Setenv("RC_JWT_TOKEN", "jwt")
	t.Setenv("RC_SERVER_URL", "https://platform.devtest.ringcentral.com")
	t.Setenv("RINGCLAW_CHAT_IDS", "c1,c2")
	t.Setenv("RINGCLAW_SOURCE_USER_IDS", "u1,u2")
	t.Setenv("RINGCLAW_CAPABILITIES", "video,phone")

	opts := onboardOptions{ChatIDs: []string{"c0"}, SourceUserIDs: []string{"u0"}, Capabilities: []string{"call_log"}}
	opts.applyEnv()

	if opts.BotID != "bot-a" || opts.TenantID != "tenant-1" || opts.OwnerUserID != "owner-1" || opts.ConversationNS != "tenant-1/bot-a" {
		t.Fatalf("applyEnv() did not populate bot identity: %#v", opts)
	}
	if opts.BotToken != "bot" || opts.ClientID != "cid" || opts.ClientSecret != "secret" || opts.JWTToken != "jwt" {
		t.Fatalf("applyEnv() did not populate credentials: %#v", opts)
	}
	if opts.ServerURL != "https://platform.devtest.ringcentral.com" {
		t.Fatalf("ServerURL = %q", opts.ServerURL)
	}
	if strings.Join(opts.ChatIDs, ",") != "c0,c1,c2" {
		t.Fatalf("ChatIDs = %#v", opts.ChatIDs)
	}
	if strings.Join(opts.SourceUserIDs, ",") != "u0,u1,u2" {
		t.Fatalf("SourceUserIDs = %#v", opts.SourceUserIDs)
	}
	if strings.Join(opts.Capabilities, ",") != "call_log,video,phone" {
		t.Fatalf("Capabilities = %#v", opts.Capabilities)
	}
}

func TestNormalizeCapabilitiesAndRequiredScopes(t *testing.T) {
	capabilities, err := normalizeCapabilities([]string{"video", "phone", "call-log", "VIDEO"})
	if err != nil {
		t.Fatalf("normalizeCapabilities() error = %v", err)
	}
	if strings.Join(capabilities, ",") != "video,phone,call_log" {
		t.Fatalf("capabilities = %#v", capabilities)
	}
	scopes := requiredScopesForCapabilities(capabilities)
	if strings.Join(scopes, ",") != "Video,RingOut,ReadCallLog" {
		t.Fatalf("scopes = %#v", scopes)
	}

	if _, err := normalizeCapabilities([]string{"fax"}); err == nil {
		t.Fatal("expected unsupported capability error")
	}
}

func TestPrintOnboardResultJSON(t *testing.T) {
	var out bytes.Buffer
	cmd := onboardCmd
	cmd.SetOut(&out)
	oldJSON := jsonOutput
	jsonOutput = true
	defer func() {
		jsonOutput = oldJSON
		cmd.SetOut(nil)
	}()

	err := printOnboardResult(cmd, onboardResult{
		Saved:          true,
		ConfigPath:     "/tmp/config.json",
		ServerURL:      "https://platform.ringcentral.com",
		BotConfigured:  true,
		BotExtensionID: "101",
		ChatIDs:        []string{"c1"},
		Capabilities:   []string{"video", "phone"},
		RequiredScopes: []string{"Video", "RingOut", "ReadCallLog"},
	})
	if err != nil {
		t.Fatalf("printOnboardResult() error = %v", err)
	}

	var got onboardResult
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v; output=%s", err, out.String())
	}
	if !got.Saved || got.BotExtensionID != "101" || len(got.ChatIDs) != 1 {
		t.Fatalf("unexpected JSON result: %#v", got)
	}
	if strings.Join(got.Capabilities, ",") != "video,phone" || strings.Join(got.RequiredScopes, ",") != "Video,RingOut,ReadCallLog" {
		t.Fatalf("unexpected capability result: %#v", got)
	}
}

func TestMergeManifestBot_DefaultsAndOverrides(t *testing.T) {
	defFalse := false
	botTrue := true
	got := mergeManifestBot(
		onboardManifestBot{
			TenantID:         "tenant",
			ServerURL:        "https://platform.ringcentral.com",
			ChatIDs:          []string{"default-chat"},
			SourceUserIDs:    []string{"owner"},
			Capabilities:     []string{"video"},
			GroupMentionOnly: &defFalse,
			DefaultAgent:     "codex",
			Agents: map[string]config.AgentConfig{
				"codex": {Type: "http", Endpoint: "https://default.example.com"},
			},
		},
		onboardManifestBot{
			BotID:            "bot-a",
			OwnerUserID:      "alice",
			ChatIDs:          []string{"bot-chat", "default-chat"},
			Capabilities:     []string{"phone", "video"},
			GroupMentionOnly: &botTrue,
			Agents: map[string]config.AgentConfig{
				"codex": {Type: "http", Endpoint: "https://bot.example.com", APIKey: "bot-key"},
			},
		},
	)

	if got.BotID != "bot-a" || got.TenantID != "tenant" || got.OwnerUserID != "alice" {
		t.Fatalf("unexpected identity merge: %#v", got)
	}
	if strings.Join(got.ChatIDs, ",") != "default-chat,bot-chat" {
		t.Fatalf("ChatIDs = %#v", got.ChatIDs)
	}
	if got.GroupMentionOnly == nil || !*got.GroupMentionOnly {
		t.Fatalf("GroupMentionOnly override not applied: %#v", got.GroupMentionOnly)
	}
	if strings.Join(got.Capabilities, ",") != "video,phone" {
		t.Fatalf("Capabilities = %#v", got.Capabilities)
	}
	if got.Agents["codex"].APIKey != "bot-key" {
		t.Fatalf("agent override not applied: %#v", got.Agents)
	}
}

func TestLoadOnboardManifest_ExpandsEnv(t *testing.T) {
	t.Setenv("BOT_A_TOKEN", "bot-token-a")
	t.Setenv("BOT_A_AGENT_TOKEN", "agent-token-a")
	path := filepath.Join(t.TempDir(), "bots.json")
	body := `{
	  "defaults": {
	    "tenant_id": "fiji",
	    "server_url": "https://platform.ringcentral.com",
	    "agents": {
	      "codex": {
	        "type": "http",
	        "endpoint": "https://agent.example.com",
	        "api_key": "${BOT_A_AGENT_TOKEN}"
	      }
	    }
	  },
	  "bots": [
	    {
	      "bot_id": "personal-ava-a",
	      "owner_user_id": "alice",
	      "bot_token": "${BOT_A_TOKEN}",
	      "capabilities": ["video", "phone"],
	      "chat_ids": ["c1"]
	    }
	  ]
	}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	manifest, err := loadOnboardManifest(path)
	if err != nil {
		t.Fatalf("loadOnboardManifest() error = %v", err)
	}
	got := mergeManifestBot(manifest.Defaults, manifest.Bots[0])
	if got.BotToken != "bot-token-a" {
		t.Fatalf("BotToken = %q", got.BotToken)
	}
	if strings.Join(got.Capabilities, ",") != "video,phone" {
		t.Fatalf("Capabilities = %#v", got.Capabilities)
	}
	if got.Agents["codex"].APIKey != "agent-token-a" {
		t.Fatalf("agent api key = %q", got.Agents["codex"].APIKey)
	}
}

func TestRenderKubernetesManifest_MountsConfigSecretAsLongLivedBot(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.DefaultAgent = "codex"
	cfg.Bot.ID = "personal-ava-summer"
	cfg.Bot.TenantID = "fiji"
	cfg.Bot.OwnerUserID = "summer.gan"
	cfg.RC.ServerURL = "https://platform.ringcentral.com"
	cfg.RC.BotToken = "bot-token"
	cfg.RC.ClientID = "client-id"
	cfg.RC.ClientSecret = "client-secret"
	cfg.RC.JWTToken = "jwt-token"
	cfg.RC.ChatIDs = []string{"123"}
	cfg.RC.SourceUserIDs = []string{"summer.gan"}
	cfg.RC.Capabilities = []string{"video", "phone"}

	body, err := renderKubernetesManifest(kubernetesRenderOptions{
		Namespace: "ava",
		Image:     "ghcr.io/ringclaw/ringclaw:test",
	}, cfg)
	if err != nil {
		t.Fatalf("renderKubernetesManifest() error = %v", err)
	}
	for _, want := range []string{
		"kind: Secret",
		"name: personal-ava-summer-config",
		"namespace: ava",
		"config.json: |",
		`"bot_token": "bot-token"`,
		`"capabilities": [`,
		`"video"`,
		`"phone"`,
		"kind: Deployment",
		"name: personal-ava-summer",
		"replicas: 1",
		"image: ghcr.io/ringclaw/ringclaw:test",
		"- start",
		"- -f",
		"name: RINGCLAW_CONFIG",
		"value: /etc/ringclaw/config/config.json",
		"mountPath: /etc/ringclaw/config/config.json",
		"subPath: config.json",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("rendered manifest missing %q:\n%s", want, body)
		}
	}
}

func TestRunOnboardManifest_RenderKubernetesManifestFiles(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "bots.json")
	outputDir := filepath.Join(dir, "rendered")
	body := `{
	  "defaults": {
	    "tenant_id": "fiji",
	    "server_url": "https://platform.ringcentral.com",
	    "default_agent": "codex"
	  },
	  "bots": [
	    {
	      "bot_id": "personal-ava-summer",
	      "owner_user_id": "summer.gan",
	      "bot_token": "bot-token",
	      "capabilities": ["video", "phone"],
	      "chat_ids": ["123"],
	      "source_user_ids": ["summer.gan"]
	    }
	  ]
	}`
	if err := os.WriteFile(manifestPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	var out bytes.Buffer
	cmd := onboardCmd
	cmd.SetOut(&out)
	oldJSON := jsonOutput
	jsonOutput = false
	defer func() {
		jsonOutput = oldJSON
		cmd.SetOut(nil)
	}()

	err := runOnboardManifest(cmd, onboardOptions{
		Manifest:            manifestPath,
		OutputDir:           outputDir,
		RenderKubernetes:    true,
		KubernetesImage:     "ghcr.io/ringclaw/ringclaw:test",
		KubernetesNamespace: "ava",
		GroupMentionOnly:    true,
	})
	if err != nil {
		t.Fatalf("runOnboardManifest() error = %v", err)
	}

	configPath := filepath.Join(outputDir, "personal-ava-summer", "config.json")
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("expected rendered config %s: %v", configPath, err)
	}
	k8sPath := filepath.Join(outputDir, "personal-ava-summer", "k8s.yaml")
	data, err := os.ReadFile(k8sPath)
	if err != nil {
		t.Fatalf("expected rendered k8s manifest %s: %v", k8sPath, err)
	}
	got := string(data)
	for _, want := range []string{
		"kind: Secret",
		"name: personal-ava-summer-config",
		"namespace: ava",
		"kind: Deployment",
		"image: ghcr.io/ringclaw/ringclaw:test",
		"value: /etc/ringclaw/config/config.json",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("rendered k8s manifest missing %q:\n%s", want, got)
		}
	}
	if !strings.Contains(out.String(), "k8s="+k8sPath) {
		t.Fatalf("summary should include k8s path, got %q", out.String())
	}
}
