package cmd

import (
	"net/url"
	"strings"
	"testing"
)

func TestBuildDeveloperAppURLForBot(t *testing.T) {
	raw := buildDeveloperAppURL(developerAppURLSpec{
		Name:        "Personal AVA Pro Bot",
		Description: "RingClaw bot",
		Type:        "ServerBot",
		Public:      false,
		Permissions: ringclawAppPermissions(),
	})

	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	query := parsed.Query()
	if parsed.Scheme != "https" || parsed.Host != "developers.ringcentral.com" || parsed.Path != "/new-app" {
		t.Fatalf("unexpected URL base: %s", raw)
	}
	if query.Get("type") != "ServerBot" {
		t.Fatalf("type = %q, want ServerBot", query.Get("type"))
	}
	if query.Get("public") != "false" {
		t.Fatalf("public = %q, want false", query.Get("public"))
	}
	if query.Get("name") != "Personal AVA Pro Bot" {
		t.Fatalf("name = %q", query.Get("name"))
	}
	for _, permission := range ringclawAppPermissions() {
		if !strings.Contains(query.Get("permissions"), permission) {
			t.Fatalf("permissions %q missing %q", query.Get("permissions"), permission)
		}
	}
	if strings.Contains(query.Get("permissions"), "SubscriptionWebhook") {
		t.Fatalf("permissions %q should use WebSocketsSubscription, not SubscriptionWebhook", query.Get("permissions"))
	}
}

func TestBuildDeveloperAppURLForPrivateJWTApp(t *testing.T) {
	raw := buildDeveloperAppURL(developerAppURLSpec{
		Name:        "Personal AVA Pro Private",
		Description: "RingClaw private app",
		Type:        "JWT",
		Public:      false,
		GrantType:   "jwt",
		Permissions: ringclawAppPermissions(),
	})

	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	query := parsed.Query()
	if query.Get("type") != "JWT" {
		t.Fatalf("type = %q, want JWT", query.Get("type"))
	}
	if query.Get("grant-type") != "jwt" {
		t.Fatalf("grant-type = %q, want jwt", query.Get("grant-type"))
	}
	if query.Get("public") != "false" {
		t.Fatalf("public = %q, want false", query.Get("public"))
	}
}

func TestRingClawAppPermissionsForVideoPhoneCapabilities(t *testing.T) {
	perms := ringclawAppPermissionsForCapabilities([]string{"video", "phone"})
	for _, permission := range []string{"Video", "RingOut", "ReadCallLog"} {
		if !containsString(perms, permission) {
			t.Fatalf("permissions %#v missing %q", perms, permission)
		}
	}
}

func TestAppURLCapabilitiesApplyOnlyToPrivateJWT(t *testing.T) {
	botPerms := ringclawBotAppPermissions([]string{"video", "phone"})
	for _, permission := range []string{"Video", "RingOut", "ReadCallLog"} {
		if containsString(botPerms, permission) {
			t.Fatalf("bot app permissions %#v should not include capability permission %q", botPerms, permission)
		}
	}

	privatePerms := ringclawPrivateAppPermissions([]string{"video", "phone"})
	for _, permission := range []string{"Video", "RingOut", "ReadCallLog"} {
		if !containsString(privatePerms, permission) {
			t.Fatalf("private app permissions %#v missing %q", privatePerms, permission)
		}
	}
}
