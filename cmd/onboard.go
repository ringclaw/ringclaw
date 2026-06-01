package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/ringclaw/ringclaw/config"
	"github.com/ringclaw/ringclaw/ringcentral"
	"github.com/spf13/cobra"
)

var onboardOpts onboardOptions

type onboardOptions struct {
	BotID               string
	TenantID            string
	OwnerUserID         string
	ConversationNS      string
	BotToken            string
	ClientID            string
	ClientSecret        string
	JWTToken            string
	ServerURL           string
	ChatIDs             []string
	SourceUserIDs       []string
	Capabilities        []string
	GroupMentionOnly    bool
	FromEnv             bool
	SkipValidate        bool
	NoSave              bool
	ConfigOut           string
	Manifest            string
	OutputDir           string
	RenderKubernetes    bool
	KubernetesImage     string
	KubernetesNamespace string
	DiscoverOwnerDM     bool
	AllowPartialUpdate  bool
}

type onboardResult struct {
	BotID                  string   `json:"bot_id,omitempty"`
	TenantID               string   `json:"tenant_id,omitempty"`
	OwnerUserID            string   `json:"owner_user_id,omitempty"`
	ConversationNamespace  string   `json:"conversation_namespace,omitempty"`
	ConfigPath             string   `json:"config_path,omitempty"`
	KubernetesManifestPath string   `json:"kubernetes_manifest_path,omitempty"`
	Saved                  bool     `json:"saved"`
	ServerURL              string   `json:"server_url"`
	BotConfigured          bool     `json:"bot_configured"`
	BotExtensionID         string   `json:"bot_extension_id,omitempty"`
	PrivateConfigured      bool     `json:"private_configured"`
	PrivateOwnerID         string   `json:"private_owner_id,omitempty"`
	DiscoveredChatID       string   `json:"discovered_chat_id,omitempty"`
	ChatIDs                []string `json:"chat_ids,omitempty"`
	SourceUserIDs          []string `json:"source_user_ids,omitempty"`
	Capabilities           []string `json:"capabilities,omitempty"`
	RequiredScopes         []string `json:"required_scopes,omitempty"`
	CapabilityWarnings     []string `json:"capability_warnings,omitempty"`
	NextSteps              []string `json:"next_steps,omitempty"`
}

type onboardManifest struct {
	Defaults onboardManifestBot   `json:"defaults,omitempty"`
	Bots     []onboardManifestBot `json:"bots"`
}

type onboardManifestBot struct {
	BotID                 string                        `json:"bot_id,omitempty"`
	TenantID              string                        `json:"tenant_id,omitempty"`
	OwnerUserID           string                        `json:"owner_user_id,omitempty"`
	ConversationNamespace string                        `json:"conversation_namespace,omitempty"`
	BotToken              string                        `json:"bot_token,omitempty"`
	ClientID              string                        `json:"client_id,omitempty"`
	ClientSecret          string                        `json:"client_secret,omitempty"`
	JWTToken              string                        `json:"jwt_token,omitempty"`
	ServerURL             string                        `json:"server_url,omitempty"`
	ChatIDs               []string                      `json:"chat_ids,omitempty"`
	SourceUserIDs         []string                      `json:"source_user_ids,omitempty"`
	Capabilities          []string                      `json:"capabilities,omitempty"`
	GroupMentionOnly      *bool                         `json:"group_mention_only,omitempty"`
	DefaultAgent          string                        `json:"default_agent,omitempty"`
	AgentWorkspace        string                        `json:"agent_workspace,omitempty"`
	APIAddr               string                        `json:"api_addr,omitempty"`
	Agents                map[string]config.AgentConfig `json:"agents,omitempty"`
}

func init() {
	onboardCmd.Flags().StringVar(&onboardOpts.BotID, "bot-id", "", "Stable bot ID for multi-bot deployments")
	onboardCmd.Flags().StringVar(&onboardOpts.TenantID, "tenant-id", "", "Tenant/account namespace for the bot")
	onboardCmd.Flags().StringVar(&onboardOpts.OwnerUserID, "owner-user-id", "", "Business owner/user ID for the bot")
	onboardCmd.Flags().StringVar(&onboardOpts.ConversationNS, "conversation-namespace", "", "Explicit agent conversation namespace; defaults to tenant-id/bot-id")
	onboardCmd.Flags().StringVar(&onboardOpts.BotToken, "bot-token", "", "RingCentral Bot token from an installed Bot app")
	onboardCmd.Flags().StringVar(&onboardOpts.ClientID, "client-id", "", "Private App Client ID")
	onboardCmd.Flags().StringVar(&onboardOpts.ClientSecret, "client-secret", "", "Private App Client Secret")
	onboardCmd.Flags().StringVar(&onboardOpts.JWTToken, "jwt-token", "", "Private App JWT credential")
	onboardCmd.Flags().StringVar(&onboardOpts.ServerURL, "server-url", "", "RingCentral server URL")
	onboardCmd.Flags().StringSliceVar(&onboardOpts.ChatIDs, "chat-id", nil, "Chat ID to monitor; may be repeated or comma-separated")
	onboardCmd.Flags().StringSliceVar(&onboardOpts.SourceUserIDs, "source-user-id", nil, "Trusted sender ID/email/phone; may be repeated or comma-separated")
	onboardCmd.Flags().StringSliceVar(&onboardOpts.Capabilities, "capability", nil, "Optional AVA capability to enable: video, phone, call_log; may be repeated or comma-separated")
	onboardCmd.Flags().BoolVar(&onboardOpts.GroupMentionOnly, "group-mention-only", true, "Require @mention in group chats")
	onboardCmd.Flags().BoolVar(&onboardOpts.FromEnv, "from-env", false, "Read RC_* and RINGCLAW_* onboarding values from environment")
	onboardCmd.Flags().BoolVar(&onboardOpts.SkipValidate, "skip-validate", false, "Write config without calling RingCentral APIs")
	onboardCmd.Flags().BoolVar(&onboardOpts.NoSave, "no-save", false, "Validate and print summary without writing config")
	onboardCmd.Flags().StringVar(&onboardOpts.ConfigOut, "config-out", "", "Write config to this path instead of RINGCLAW_CONFIG or ~/.ringclaw/config.json")
	onboardCmd.Flags().StringVar(&onboardOpts.Manifest, "manifest", "", "JSON manifest describing multiple long-lived bots to render")
	onboardCmd.Flags().StringVar(&onboardOpts.OutputDir, "output-dir", "", "Directory for --manifest rendered config files")
	onboardCmd.Flags().BoolVar(&onboardOpts.RenderKubernetes, "k8s", false, "Render a Kubernetes Secret and Deployment manifest next to each bot config")
	onboardCmd.Flags().StringVar(&onboardOpts.KubernetesImage, "k8s-image", "ghcr.io/ringclaw/ringclaw:latest", "Container image to use in rendered Kubernetes Deployment")
	onboardCmd.Flags().StringVar(&onboardOpts.KubernetesNamespace, "k8s-namespace", "default", "Kubernetes namespace for rendered manifests")
	onboardCmd.Flags().BoolVar(&onboardOpts.DiscoverOwnerDM, "discover-owner-dm", true, "Use APIs to discover/create the owner-bot direct chat when Private App credentials are present")
	onboardCmd.Flags().BoolVar(&onboardOpts.AllowPartialUpdate, "allow-partial-update", false, "Allow updating non-credential fields without providing a bot token")
	rootCmd.AddCommand(onboardCmd)
}

var onboardCmd = &cobra.Command{
	Use:   "onboard",
	Short: "API-assisted RingCentral credential onboarding",
	Long: `Validate RingCentral credentials through the public APIs and write the
RingClaw config non-interactively.

This command cannot create Developer Console apps or JWT credentials because
RingCentral does not expose public REST APIs for those resources. Use
"ringclaw app-url" to open pre-filled Developer Console creation links first,
then pass the resulting credentials to this command.`,
	RunE: runOnboard,
}

func runOnboard(cmd *cobra.Command, args []string) error {
	opts := onboardOpts
	if opts.FromEnv {
		opts.applyEnv()
	}
	if opts.Manifest != "" {
		return runOnboardManifest(cmd, opts)
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if cfg == nil {
		cfg = config.DefaultConfig()
	}

	if opts.ServerURL == "" {
		opts.ServerURL = cfg.RC.ServerURL
	}
	if opts.ServerURL == "" {
		opts.ServerURL = "https://platform.ringcentral.com"
	}

	if opts.BotToken == "" {
		opts.BotToken = cfg.RC.BotToken
	}
	if opts.ClientID == "" {
		opts.ClientID = cfg.RC.ClientID
	}
	if opts.ClientSecret == "" {
		opts.ClientSecret = cfg.RC.ClientSecret
	}
	if opts.JWTToken == "" {
		opts.JWTToken = cfg.RC.JWTToken
	}
	if opts.BotID == "" {
		opts.BotID = cfg.Bot.ID
	}
	if opts.TenantID == "" {
		opts.TenantID = cfg.Bot.TenantID
	}
	if opts.OwnerUserID == "" {
		opts.OwnerUserID = cfg.Bot.OwnerUserID
	}
	if opts.ConversationNS == "" {
		opts.ConversationNS = cfg.Bot.ConversationNamespace
	}

	if opts.BotToken == "" && !opts.AllowPartialUpdate {
		return fmt.Errorf("bot token is required; create/install a Bot app first or pass --allow-partial-update")
	}
	if err := validatePrivateCredentialSet(opts.ClientID, opts.ClientSecret, opts.JWTToken); err != nil {
		return err
	}
	if !opts.AllowPartialUpdate && !hasPrivateCredentialSet(opts.ClientID, opts.ClientSecret, opts.JWTToken) {
		return fmt.Errorf("private app credentials are required for RingClaw onboarding: --client-id, --client-secret, --jwt-token")
	}

	cfg.Bot.ID = opts.BotID
	cfg.Bot.TenantID = opts.TenantID
	cfg.Bot.OwnerUserID = opts.OwnerUserID
	cfg.Bot.ConversationNamespace = opts.ConversationNS
	cfg.RC.ServerURL = opts.ServerURL
	cfg.RC.BotToken = opts.BotToken
	cfg.RC.ClientID = opts.ClientID
	cfg.RC.ClientSecret = opts.ClientSecret
	cfg.RC.JWTToken = opts.JWTToken
	cfg.RC.ChatIDs = mergeStrings(cfg.RC.ChatIDs, opts.ChatIDs)
	cfg.RC.SourceUserIDs = mergeStrings(cfg.RC.SourceUserIDs, opts.SourceUserIDs)
	capabilities, err := normalizeCapabilities(mergeStrings(cfg.RC.Capabilities, opts.Capabilities))
	if err != nil {
		return err
	}
	cfg.RC.Capabilities = capabilities
	groupMentionOnly := opts.GroupMentionOnly
	cfg.RC.GroupMentionOnly = &groupMentionOnly

	requiredScopes := requiredScopesForCapabilities(cfg.RC.Capabilities)
	result := onboardResult{
		BotID:                 cfg.Bot.ID,
		TenantID:              cfg.Bot.TenantID,
		OwnerUserID:           cfg.Bot.OwnerUserID,
		ConversationNamespace: cfg.Bot.EffectiveConversationNamespace(),
		ServerURL:             cfg.RC.ServerURL,
		BotConfigured:         cfg.RC.BotToken != "",
		PrivateConfigured:     cfg.RC.HasPrivateApp(),
		ChatIDs:               cfg.RC.ChatIDs,
		SourceUserIDs:         cfg.RC.SourceUserIDs,
		Capabilities:          cfg.RC.Capabilities,
		RequiredScopes:        requiredScopes,
	}
	if !opts.SkipValidate {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if cfg.RC.BotToken != "" {
			botClient := ringcentral.NewBotClient(cfg.RC.ServerURL, cfg.RC.BotToken)
			botID, err := botClient.GetExtensionInfo(ctx)
			if err != nil {
				return fmt.Errorf("validate bot token: %w", err)
			}
			botClient.SetOwnerID(botID)
			result.BotExtensionID = botID

			if cfg.RC.HasPrivateApp() {
				privateClient := ringcentral.NewClient(&ringcentral.Credentials{
					ClientID:       cfg.RC.ClientID,
					ClientSecret:   cfg.RC.ClientSecret,
					JWTToken:       cfg.RC.JWTToken,
					ServerURL:      cfg.RC.ServerURL,
					VideoServerURL: cfg.RC.VideoServerURL,
				})
				if err := privateClient.Authenticate(); err != nil {
					return fmt.Errorf("validate private app credentials: %w", err)
				}
				ownerID, err := privateClient.GetExtensionInfo(ctx)
				if err != nil {
					return fmt.Errorf("read private app owner: %w", err)
				}
				privateClient.SetOwnerID(ownerID)
				result.PrivateOwnerID = ownerID

				if opts.DiscoverOwnerDM {
					dmChatID, err := botClient.FindDirectChat(ctx, ownerID)
					if err != nil {
						return fmt.Errorf("discover owner-bot DM chat: %w", err)
					}
					cfg.RC.ChatIDs = mergeStrings(cfg.RC.ChatIDs, []string{dmChatID})
					result.DiscoveredChatID = dmChatID
					result.ChatIDs = cfg.RC.ChatIDs
				}
			}
		}
	}

	result.NextSteps = []string{
		"Run `ringclaw start` to start the message bridge.",
		"Use `ringclaw chat list --recent` if you need more chat IDs.",
	}
	if len(requiredScopes) > 0 {
		result.NextSteps = append(result.NextSteps,
			fmt.Sprintf("Ensure the selected RingCentral app has scopes: %s.", strings.Join(requiredScopes, ", ")),
			fmt.Sprintf("To generate matching Developer Console links, run: ringclaw app-url %s.", capabilityFlags(cfg.RC.Capabilities)),
		)
	}

	if !opts.NoSave {
		configPath := opts.ConfigOut
		if configPath == "" {
			configPath, _ = config.ConfigPath()
		}
		if err := config.SaveTo(configPath, cfg); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
		result.Saved = true
		result.ConfigPath = configPath
		if opts.RenderKubernetes {
			manifest, err := renderKubernetesManifest(kubernetesRenderOptions{
				Namespace: opts.KubernetesNamespace,
				Image:     opts.KubernetesImage,
			}, cfg)
			if err != nil {
				return err
			}
			k8sPath := filepath.Join(filepath.Dir(configPath), "k8s.yaml")
			if err := os.WriteFile(k8sPath, []byte(manifest), 0o600); err != nil {
				return fmt.Errorf("save kubernetes manifest: %w", err)
			}
			result.KubernetesManifestPath = k8sPath
		}
	}

	return printOnboardResult(cmd, result)
}

func runOnboardManifest(cmd *cobra.Command, opts onboardOptions) error {
	if opts.OutputDir == "" && !opts.NoSave {
		return fmt.Errorf("--output-dir is required with --manifest unless --no-save is set")
	}
	manifest, err := loadOnboardManifest(opts.Manifest)
	if err != nil {
		return err
	}
	if len(manifest.Bots) == 0 {
		return fmt.Errorf("manifest has no bots")
	}

	results := make([]onboardResult, 0, len(manifest.Bots))
	for _, bot := range manifest.Bots {
		merged := mergeManifestBot(manifest.Defaults, bot)
		if merged.BotID == "" {
			return fmt.Errorf("manifest bot is missing bot_id")
		}

		cfg := config.DefaultConfig()
		cfg.Bot.ID = merged.BotID
		cfg.Bot.TenantID = merged.TenantID
		cfg.Bot.OwnerUserID = merged.OwnerUserID
		cfg.Bot.ConversationNamespace = merged.ConversationNamespace
		cfg.DefaultAgent = merged.DefaultAgent
		cfg.AgentWorkspace = merged.AgentWorkspace
		cfg.APIAddr = merged.APIAddr
		if len(merged.Agents) > 0 {
			cfg.Agents = merged.Agents
		}
		cfg.RC.ServerURL = firstNonEmpty(merged.ServerURL, "https://platform.ringcentral.com")
		cfg.RC.BotToken = merged.BotToken
		cfg.RC.ClientID = merged.ClientID
		cfg.RC.ClientSecret = merged.ClientSecret
		cfg.RC.JWTToken = merged.JWTToken
		cfg.RC.ChatIDs = mergeStrings(nil, merged.ChatIDs)
		cfg.RC.SourceUserIDs = mergeStrings(nil, merged.SourceUserIDs)
		capabilities, err := normalizeCapabilities(merged.Capabilities)
		if err != nil {
			return fmt.Errorf("manifest bot %q: %w", merged.BotID, err)
		}
		cfg.RC.Capabilities = capabilities
		if merged.GroupMentionOnly != nil {
			cfg.RC.GroupMentionOnly = merged.GroupMentionOnly
		} else {
			v := opts.GroupMentionOnly
			cfg.RC.GroupMentionOnly = &v
		}

		if cfg.RC.BotToken == "" && !opts.AllowPartialUpdate {
			return fmt.Errorf("manifest bot %q missing bot_token", merged.BotID)
		}
		if err := validatePrivateCredentialSet(cfg.RC.ClientID, cfg.RC.ClientSecret, cfg.RC.JWTToken); err != nil {
			return fmt.Errorf("manifest bot %q: %w", merged.BotID, err)
		}
		if !opts.AllowPartialUpdate && !cfg.RC.HasPrivateApp() {
			return fmt.Errorf("manifest bot %q missing private app credentials", merged.BotID)
		}

		requiredScopes := requiredScopesForCapabilities(cfg.RC.Capabilities)
		result := onboardResult{
			BotID:                 cfg.Bot.ID,
			TenantID:              cfg.Bot.TenantID,
			OwnerUserID:           cfg.Bot.OwnerUserID,
			ConversationNamespace: cfg.Bot.EffectiveConversationNamespace(),
			ServerURL:             cfg.RC.ServerURL,
			BotConfigured:         cfg.RC.BotToken != "",
			PrivateConfigured:     cfg.RC.HasPrivateApp(),
			ChatIDs:               cfg.RC.ChatIDs,
			SourceUserIDs:         cfg.RC.SourceUserIDs,
			Capabilities:          cfg.RC.Capabilities,
			RequiredScopes:        requiredScopes,
			NextSteps: []string{
				"Mount this config into a RingClaw pod and run `ringclaw start -f`.",
			},
		}
		if len(requiredScopes) > 0 {
			result.NextSteps = append(result.NextSteps,
				fmt.Sprintf("Ensure the selected RingCentral app has scopes: %s.", strings.Join(requiredScopes, ", ")),
			)
		}
		if !opts.NoSave {
			botDir := filepath.Join(opts.OutputDir, sanitizeFileName(merged.BotID))
			configPath := filepath.Join(botDir, "config.json")
			if err := config.SaveTo(configPath, cfg); err != nil {
				return fmt.Errorf("save manifest bot %q config: %w", merged.BotID, err)
			}
			result.Saved = true
			result.ConfigPath = configPath
			if opts.RenderKubernetes {
				manifest, err := renderKubernetesManifest(kubernetesRenderOptions{
					Namespace: opts.KubernetesNamespace,
					Image:     opts.KubernetesImage,
				}, cfg)
				if err != nil {
					return fmt.Errorf("render manifest bot %q kubernetes manifest: %w", merged.BotID, err)
				}
				k8sPath := filepath.Join(botDir, "k8s.yaml")
				if err := os.WriteFile(k8sPath, []byte(manifest), 0o600); err != nil {
					return fmt.Errorf("save manifest bot %q kubernetes manifest: %w", merged.BotID, err)
				}
				result.KubernetesManifestPath = k8sPath
			}
		}

		results = append(results, result)
	}

	if jsonOutput {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(results)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Rendered %d RingClaw bot configs\n", len(results))
	for _, result := range results {
		fmt.Fprintf(cmd.OutOrStdout(), "  - %s", result.BotID)
		if result.TenantID != "" {
			fmt.Fprintf(cmd.OutOrStdout(), " (%s)", result.TenantID)
		}
		if result.ConfigPath != "" {
			fmt.Fprintf(cmd.OutOrStdout(), ": %s", result.ConfigPath)
		}
		if result.KubernetesManifestPath != "" {
			fmt.Fprintf(cmd.OutOrStdout(), " k8s=%s", result.KubernetesManifestPath)
		}
		fmt.Fprintln(cmd.OutOrStdout())
	}
	return nil
}

func loadOnboardManifest(path string) (*onboardManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	var manifest onboardManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	expandEnvStrings(&manifest)
	return &manifest, nil
}

func mergeManifestBot(defaults, bot onboardManifestBot) onboardManifestBot {
	out := defaults
	if bot.BotID != "" {
		out.BotID = bot.BotID
	}
	if bot.TenantID != "" {
		out.TenantID = bot.TenantID
	}
	if bot.OwnerUserID != "" {
		out.OwnerUserID = bot.OwnerUserID
	}
	if bot.ConversationNamespace != "" {
		out.ConversationNamespace = bot.ConversationNamespace
	}
	if bot.BotToken != "" {
		out.BotToken = bot.BotToken
	}
	if bot.ClientID != "" {
		out.ClientID = bot.ClientID
	}
	if bot.ClientSecret != "" {
		out.ClientSecret = bot.ClientSecret
	}
	if bot.JWTToken != "" {
		out.JWTToken = bot.JWTToken
	}
	if bot.ServerURL != "" {
		out.ServerURL = bot.ServerURL
	}
	out.ChatIDs = mergeStrings(defaults.ChatIDs, bot.ChatIDs)
	out.SourceUserIDs = mergeStrings(defaults.SourceUserIDs, bot.SourceUserIDs)
	out.Capabilities = mergeStrings(defaults.Capabilities, bot.Capabilities)
	if bot.GroupMentionOnly != nil {
		out.GroupMentionOnly = bot.GroupMentionOnly
	}
	if bot.DefaultAgent != "" {
		out.DefaultAgent = bot.DefaultAgent
	}
	if bot.AgentWorkspace != "" {
		out.AgentWorkspace = bot.AgentWorkspace
	}
	if bot.APIAddr != "" {
		out.APIAddr = bot.APIAddr
	}
	if len(bot.Agents) > 0 {
		out.Agents = bot.Agents
	}
	return out
}

func (o *onboardOptions) applyEnv() {
	o.BotID = firstNonEmpty(o.BotID, os.Getenv("RINGCLAW_BOT_ID"))
	o.TenantID = firstNonEmpty(o.TenantID, os.Getenv("RINGCLAW_TENANT_ID"))
	o.OwnerUserID = firstNonEmpty(o.OwnerUserID, os.Getenv("RINGCLAW_OWNER_USER_ID"))
	o.ConversationNS = firstNonEmpty(o.ConversationNS, os.Getenv("RINGCLAW_CONVERSATION_NAMESPACE"))
	o.BotToken = firstNonEmpty(o.BotToken, os.Getenv("RC_BOT_TOKEN"))
	o.ClientID = firstNonEmpty(o.ClientID, os.Getenv("RC_CLIENT_ID"))
	o.ClientSecret = firstNonEmpty(o.ClientSecret, os.Getenv("RC_CLIENT_SECRET"))
	o.JWTToken = firstNonEmpty(o.JWTToken, os.Getenv("RC_JWT_TOKEN"))
	o.ServerURL = firstNonEmpty(o.ServerURL, os.Getenv("RC_SERVER_URL"))
	o.ChatIDs = mergeStrings(o.ChatIDs, splitCSV(os.Getenv("RINGCLAW_CHAT_IDS")))
	o.SourceUserIDs = mergeStrings(o.SourceUserIDs, splitCSV(os.Getenv("RINGCLAW_SOURCE_USER_IDS")))
	o.Capabilities = mergeStrings(o.Capabilities, splitCSV(os.Getenv("RINGCLAW_CAPABILITIES")))
}

func normalizeCapabilities(values []string) ([]string, error) {
	var out []string
	for _, value := range values {
		for _, item := range splitCSV(value) {
			capability := strings.ToLower(strings.TrimSpace(item))
			switch capability {
			case "":
				continue
			case "video":
				capability = "video"
			case "phone":
				capability = "phone"
			case "call_log", "calllog", "call-log":
				capability = "call_log"
			default:
				return nil, fmt.Errorf("unsupported capability %q; supported values: video, phone, call_log", item)
			}
			if !containsString(out, capability) {
				out = append(out, capability)
			}
		}
	}
	return out, nil
}

func requiredScopesForCapabilities(capabilities []string) []string {
	scopes := []string{"ReadAccounts"}
	for _, capability := range capabilities {
		switch capability {
		case "video":
			scopes = appendUnique(scopes, "Video")
		case "phone":
			scopes = appendUnique(scopes, "RingOut", "ReadCallLog")
		case "call_log":
			scopes = appendUnique(scopes, "ReadCallLog")
		}
	}
	return scopes
}

type kubernetesRenderOptions struct {
	Namespace string
	Image     string
}

func renderKubernetesManifest(opts kubernetesRenderOptions, cfg *config.Config) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("kubernetes manifest requires config")
	}
	name := sanitizeKubernetesName(cfg.Bot.ID)
	if name == "" {
		name = "ringclaw-bot"
	}
	namespace := sanitizeKubernetesName(firstNonEmpty(opts.Namespace, "default"))
	if namespace == "" {
		namespace = "default"
	}
	image := firstNonEmpty(opts.Image, "ghcr.io/ringclaw/ringclaw:latest")
	secretName := name + "-config"

	configJSON, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal kubernetes config secret: %w", err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "apiVersion: v1\n")
	fmt.Fprintf(&b, "kind: Secret\n")
	fmt.Fprintf(&b, "metadata:\n")
	fmt.Fprintf(&b, "  name: %s\n", secretName)
	fmt.Fprintf(&b, "  namespace: %s\n", namespace)
	fmt.Fprintf(&b, "  labels:\n")
	fmt.Fprintf(&b, "    app.kubernetes.io/name: ringclaw\n")
	fmt.Fprintf(&b, "    app.kubernetes.io/instance: %s\n", name)
	fmt.Fprintf(&b, "    ringclaw.ai/bot-id: %s\n", name)
	fmt.Fprintf(&b, "type: Opaque\n")
	fmt.Fprintf(&b, "stringData:\n")
	fmt.Fprintf(&b, "  config.json: |\n")
	for _, line := range strings.Split(string(configJSON), "\n") {
		fmt.Fprintf(&b, "    %s\n", line)
	}
	fmt.Fprintf(&b, "---\n")
	fmt.Fprintf(&b, "apiVersion: apps/v1\n")
	fmt.Fprintf(&b, "kind: Deployment\n")
	fmt.Fprintf(&b, "metadata:\n")
	fmt.Fprintf(&b, "  name: %s\n", name)
	fmt.Fprintf(&b, "  namespace: %s\n", namespace)
	fmt.Fprintf(&b, "  labels:\n")
	fmt.Fprintf(&b, "    app.kubernetes.io/name: ringclaw\n")
	fmt.Fprintf(&b, "    app.kubernetes.io/instance: %s\n", name)
	fmt.Fprintf(&b, "    ringclaw.ai/bot-id: %s\n", name)
	fmt.Fprintf(&b, "spec:\n")
	fmt.Fprintf(&b, "  replicas: 1\n")
	fmt.Fprintf(&b, "  selector:\n")
	fmt.Fprintf(&b, "    matchLabels:\n")
	fmt.Fprintf(&b, "      app.kubernetes.io/name: ringclaw\n")
	fmt.Fprintf(&b, "      app.kubernetes.io/instance: %s\n", name)
	fmt.Fprintf(&b, "  template:\n")
	fmt.Fprintf(&b, "    metadata:\n")
	fmt.Fprintf(&b, "      labels:\n")
	fmt.Fprintf(&b, "        app.kubernetes.io/name: ringclaw\n")
	fmt.Fprintf(&b, "        app.kubernetes.io/instance: %s\n", name)
	fmt.Fprintf(&b, "        ringclaw.ai/bot-id: %s\n", name)
	fmt.Fprintf(&b, "    spec:\n")
	fmt.Fprintf(&b, "      containers:\n")
	fmt.Fprintf(&b, "        - name: ringclaw\n")
	fmt.Fprintf(&b, "          image: %s\n", image)
	fmt.Fprintf(&b, "          imagePullPolicy: IfNotPresent\n")
	fmt.Fprintf(&b, "          args:\n")
	fmt.Fprintf(&b, "            - start\n")
	fmt.Fprintf(&b, "            - -f\n")
	fmt.Fprintf(&b, "          env:\n")
	fmt.Fprintf(&b, "            - name: RINGCLAW_CONFIG\n")
	fmt.Fprintf(&b, "              value: /etc/ringclaw/config/config.json\n")
	fmt.Fprintf(&b, "          volumeMounts:\n")
	fmt.Fprintf(&b, "            - name: config\n")
	fmt.Fprintf(&b, "              mountPath: /etc/ringclaw/config/config.json\n")
	fmt.Fprintf(&b, "              subPath: config.json\n")
	fmt.Fprintf(&b, "              readOnly: true\n")
	fmt.Fprintf(&b, "      volumes:\n")
	fmt.Fprintf(&b, "        - name: config\n")
	fmt.Fprintf(&b, "          secret:\n")
	fmt.Fprintf(&b, "            secretName: %s\n", secretName)
	return b.String(), nil
}

func capabilityFlags(capabilities []string) string {
	if len(capabilities) == 0 {
		return ""
	}
	parts := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		parts = append(parts, "--capability "+capability)
	}
	return strings.Join(parts, " ")
}

func validatePrivateCredentialSet(clientID, clientSecret, jwtToken string) error {
	values := []string{clientID, clientSecret, jwtToken}
	nonEmpty := 0
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			nonEmpty++
		}
	}
	if nonEmpty != 0 && nonEmpty != len(values) {
		return fmt.Errorf("private app credentials must be provided together: --client-id, --client-secret, --jwt-token")
	}
	return nil
}

func hasPrivateCredentialSet(clientID, clientSecret, jwtToken string) bool {
	return strings.TrimSpace(clientID) != "" && strings.TrimSpace(clientSecret) != "" && strings.TrimSpace(jwtToken) != ""
}

func printOnboardResult(cmd *cobra.Command, result onboardResult) error {
	if jsonOutput {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	fmt.Fprintln(cmd.OutOrStdout(), "RingClaw API onboarding summary")
	if result.BotID != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "  Bot ID:          %s\n", result.BotID)
	}
	if result.TenantID != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "  Tenant ID:       %s\n", result.TenantID)
	}
	if result.OwnerUserID != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "  Owner User ID:   %s\n", result.OwnerUserID)
	}
	if result.ConversationNamespace != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "  Agent Namespace: %s\n", result.ConversationNamespace)
	}
	if result.Saved {
		fmt.Fprintf(cmd.OutOrStdout(), "  Config:          %s\n", result.ConfigPath)
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), "  Config:          not saved (--no-save)")
	}
	fmt.Fprintf(cmd.OutOrStdout(), "  Server URL:      %s\n", result.ServerURL)
	if result.BotConfigured {
		fmt.Fprintf(cmd.OutOrStdout(), "  Bot:             configured")
		if result.BotExtensionID != "" {
			fmt.Fprintf(cmd.OutOrStdout(), " (extension %s)", result.BotExtensionID)
		}
		fmt.Fprintln(cmd.OutOrStdout())
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), "  Bot:             not configured")
	}
	if result.PrivateConfigured {
		fmt.Fprintf(cmd.OutOrStdout(), "  Private App:     configured")
		if result.PrivateOwnerID != "" {
			fmt.Fprintf(cmd.OutOrStdout(), " (owner %s)", result.PrivateOwnerID)
		}
		fmt.Fprintln(cmd.OutOrStdout())
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), "  Private App:     not configured")
	}
	if result.DiscoveredChatID != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "  Owner DM Chat:   %s\n", result.DiscoveredChatID)
	}
	if len(result.ChatIDs) > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "  Chat IDs:        %s\n", strings.Join(result.ChatIDs, ", "))
	}
	if len(result.SourceUserIDs) > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "  Trusted Senders: %s\n", strings.Join(result.SourceUserIDs, ", "))
	}
	if len(result.Capabilities) > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "  Capabilities:    %s\n", strings.Join(result.Capabilities, ", "))
	}
	if len(result.RequiredScopes) > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "  Required Scopes: %s\n", strings.Join(result.RequiredScopes, ", "))
	}
	for _, warning := range result.CapabilityWarnings {
		fmt.Fprintf(cmd.OutOrStdout(), "  Warning:         %s\n", warning)
	}
	for _, step := range result.NextSteps {
		fmt.Fprintf(cmd.OutOrStdout(), "  - %s\n", step)
	}
	return nil
}

func mergeStrings(existing, incoming []string) []string {
	out := make([]string, 0, len(existing)+len(incoming))
	seen := make(map[string]bool, len(existing)+len(incoming))
	for _, value := range append(existing, incoming...) {
		for _, item := range splitCSV(value) {
			key := strings.ToLower(strings.TrimSpace(item))
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, strings.TrimSpace(item))
		}
	}
	return out
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func sanitizeFileName(value string) string {
	value = strings.TrimSpace(value)
	replacer := strings.NewReplacer("/", "-", "\\", "-", ":", "-", " ", "-")
	value = replacer.Replace(value)
	if value == "" {
		return "bot"
	}
	return value
}

func sanitizeKubernetesName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func expandEnvStrings(target any) {
	expandEnvValue(reflect.ValueOf(target))
}

func expandEnvValue(v reflect.Value) {
	if !v.IsValid() {
		return
	}
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return
		}
		expandEnvValue(v.Elem())
		return
	}
	switch v.Kind() {
	case reflect.String:
		if v.CanSet() {
			v.SetString(os.ExpandEnv(v.String()))
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			expandEnvValue(v.Field(i))
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			expandEnvValue(v.Index(i))
		}
	case reflect.Map:
		for _, key := range v.MapKeys() {
			value := v.MapIndex(key)
			copyValue := reflect.New(value.Type()).Elem()
			copyValue.Set(value)
			expandEnvValue(copyValue)
			v.SetMapIndex(key, copyValue)
		}
	}
}
