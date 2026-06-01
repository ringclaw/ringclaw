package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

const developerNewAppURL = "https://developers.ringcentral.com/new-app"

var (
	appURLBotName      string
	appURLPrivateName  string
	appURLCapabilities []string
)

type developerAppURL struct {
	Kind        string   `json:"kind"`
	Name        string   `json:"name"`
	URL         string   `json:"url"`
	NextSteps   []string `json:"next_steps"`
	Limitations []string `json:"limitations,omitempty"`
}

type developerAppURLSpec struct {
	Name        string
	Description string
	Type        string
	Public      bool
	GrantType   string
	Permissions []string
}

func init() {
	appURLCmd.Flags().StringVar(&appURLBotName, "bot-name", "RingClaw Bot", "Name for the RingCentral Bot app")
	appURLCmd.Flags().StringVar(&appURLPrivateName, "private-name", "RingClaw Private App", "Name for the RingCentral private JWT app")
	appURLCmd.Flags().StringSliceVar(&appURLCapabilities, "capability", nil, "Additional capability scopes to include: video, phone, call_log, sms")
	rootCmd.AddCommand(appURLCmd)
}

var appURLCmd = &cobra.Command{
	Use:   "app-url",
	Short: "Generate pre-filled RingCentral Developer Console app creation URLs",
	Long: `Generate RingCentral Developer Console URLs that pre-fill the app settings
RingClaw needs. RingCentral does not expose a public REST endpoint for creating
Developer Console applications; these URLs take the user to the official Console
creation flow with the app type, visibility, and scopes pre-selected.`,
	RunE: runAppURL,
}

func runAppURL(cmd *cobra.Command, args []string) error {
	apps := []developerAppURL{
		{
			Kind: "bot",
			Name: appURLBotName,
			URL: buildDeveloperAppURL(developerAppURLSpec{
				Name:        appURLBotName,
				Description: "RingClaw Team Messaging bot bridge",
				Type:        "ServerBot",
				Public:      false,
				Permissions: ringclawBotAppPermissions(appURLCapabilities),
			}),
			NextSteps: []string{
				"Open the URL and sign in to the RingCentral Developer Console.",
				"Create the private Bot app, then install it from the Bot tab.",
				"Copy the Bot Token into ringcentral.bot_token.",
			},
			Limitations: []string{
				"RingCentral still requires a logged-in user to confirm creation and installation.",
			},
		},
		{
			Kind: "private_jwt",
			Name: appURLPrivateName,
			URL: buildDeveloperAppURL(developerAppURLSpec{
				Name:        appURLPrivateName,
				Description: "RingClaw private JWT app for owner authentication and cross-chat access",
				Type:        "JWT",
				Public:      false,
				GrantType:   "jwt",
				Permissions: ringclawPrivateAppPermissions(appURLCapabilities),
			}),
			NextSteps: []string{
				"Open the URL and sign in to the RingCentral Developer Console.",
				"Create the private JWT app and copy the Client ID and Client Secret.",
				"Create a JWT credential for the owner and copy it into ringcentral.jwt_token.",
			},
			Limitations: []string{
				"RingCentral still requires a logged-in user to confirm creation and generate the JWT credential.",
			},
		},
	}

	if jsonOutput {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(apps)
	}

	for _, app := range apps {
		fmt.Fprintf(cmd.OutOrStdout(), "%s (%s)\n", app.Name, app.Kind)
		fmt.Fprintln(cmd.OutOrStdout(), app.URL)
		for _, step := range app.NextSteps {
			fmt.Fprintf(cmd.OutOrStdout(), "  - %s\n", step)
		}
		if len(app.Limitations) > 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "  Limitations:")
			for _, limitation := range app.Limitations {
				fmt.Fprintf(cmd.OutOrStdout(), "  - %s\n", limitation)
			}
		}
		fmt.Fprintln(cmd.OutOrStdout())
	}
	return nil
}

func ringclawAppPermissions() []string {
	return ringclawAppPermissionsForCapabilities(nil)
}

func ringclawBotAppPermissions(_ []string) []string {
	return ringclawAppPermissions()
}

func ringclawPrivateAppPermissions(capabilities []string) []string {
	return ringclawAppPermissionsForCapabilities(capabilities)
}

func ringclawAppPermissionsForCapabilities(capabilities []string) []string {
	permissions := []string{
		"ReadAccounts",
		"ReadMessages",
		"TeamMessaging",
		"WebSocketsSubscription",
	}
	for _, capability := range capabilities {
		switch strings.ToLower(strings.TrimSpace(capability)) {
		case "":
			continue
		case "video":
			permissions = appendUnique(permissions, "Video")
		case "phone":
			permissions = appendUnique(permissions, "RingOut", "ReadCallLog")
		case "call_log", "calllog":
			permissions = appendUnique(permissions, "ReadCallLog")
		case "sms":
			permissions = appendUnique(permissions, "SMS")
		}
	}
	return permissions
}

func appendUnique(values []string, candidates ...string) []string {
	for _, candidate := range candidates {
		if !containsString(values, candidate) {
			values = append(values, candidate)
		}
	}
	return values
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func buildDeveloperAppURL(spec developerAppURLSpec) string {
	values := url.Values{}
	values.Set("name", spec.Name)
	values.Set("desc", spec.Description)
	values.Set("public", fmt.Sprintf("%t", spec.Public))
	values.Set("type", spec.Type)
	if spec.GrantType != "" {
		values.Set("grant-type", spec.GrantType)
	}
	if len(spec.Permissions) > 0 {
		values.Set("permissions", joinComma(spec.Permissions))
	}
	return developerNewAppURL + "?" + values.Encode()
}

func joinComma(values []string) string {
	if len(values) == 0 {
		return ""
	}
	out := values[0]
	for _, value := range values[1:] {
		out += "," + value
	}
	return out
}
