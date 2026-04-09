package messaging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMemoryStore_AddAndList(t *testing.T) {
	dir := t.TempDir()
	store := NewMemoryStore(dir)

	if err := store.Add("Go 1.22 is required"); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if err := store.Add("Use pnpm over npm"); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	list := store.List()
	if !strings.Contains(list, "Go 1.22 is required") {
		t.Errorf("list missing first entry: %s", list)
	}
	if !strings.Contains(list, "Use pnpm over npm") {
		t.Errorf("list missing second entry: %s", list)
	}
	if !strings.Contains(list, "2 entries") {
		t.Errorf("wrong count in list: %s", list)
	}
}

func TestMemoryStore_Delete(t *testing.T) {
	dir := t.TempDir()
	store := NewMemoryStore(dir)

	store.Add("entry one")
	store.Add("entry two")
	store.Add("entry three")

	if err := store.Delete(2); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	list := store.List()
	if strings.Contains(list, "entry two") {
		t.Errorf("deleted entry still present: %s", list)
	}
	if !strings.Contains(list, "entry one") || !strings.Contains(list, "entry three") {
		t.Errorf("other entries missing: %s", list)
	}
}

func TestMemoryStore_DeleteInvalidIndex(t *testing.T) {
	dir := t.TempDir()
	store := NewMemoryStore(dir)

	store.Add("only entry")

	if err := store.Delete(0); err == nil {
		t.Error("expected error for index 0")
	}
	if err := store.Delete(2); err == nil {
		t.Error("expected error for index 2")
	}
}

func TestMemoryStore_EmptyList(t *testing.T) {
	dir := t.TempDir()
	store := NewMemoryStore(dir)

	list := store.List()
	if list != "No memories stored." {
		t.Errorf("unexpected list for empty store: %s", list)
	}
}

func TestMemoryStore_Count(t *testing.T) {
	dir := t.TempDir()
	store := NewMemoryStore(dir)

	if store.Count() != 0 {
		t.Errorf("expected 0, got %d", store.Count())
	}
	store.Add("one")
	store.Add("two")
	if store.Count() != 2 {
		t.Errorf("expected 2, got %d", store.Count())
	}
}

func TestMemoryStore_FileFormat(t *testing.T) {
	dir := t.TempDir()
	store := NewMemoryStore(dir)

	store.Add("test entry")

	data, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatalf("read file failed: %v", err)
	}
	content := string(data)
	if !strings.HasPrefix(content, "# RingClaw Shared Memory") {
		t.Errorf("missing header: %s", content)
	}
	if !strings.Contains(content, "## Memories") {
		t.Errorf("missing section: %s", content)
	}
	if !strings.Contains(content, "- [") {
		t.Errorf("missing timestamped entry: %s", content)
	}
}

func TestHandleMemoryCommand(t *testing.T) {
	dir := t.TempDir()
	store := NewMemoryStore(dir)

	result := HandleMemoryCommand(nil, "/mem")
	if !strings.Contains(result, "not configured") {
		t.Errorf("expected not configured: %s", result)
	}

	result = HandleMemoryCommand(store, "/mem add Go 1.22")
	if !strings.Contains(result, "Remembered") {
		t.Errorf("unexpected result: %s", result)
	}

	result = HandleMemoryCommand(store, "/mem list")
	if !strings.Contains(result, "Go 1.22") {
		t.Errorf("memory not listed: %s", result)
	}

	result = HandleMemoryCommand(store, "/mem")
	if !strings.Contains(result, "Go 1.22") {
		t.Errorf("/mem without subcommand should list: %s", result)
	}

	result = HandleMemoryCommand(store, "/mem del 1")
	if !strings.Contains(result, "Forgot") {
		t.Errorf("unexpected result: %s", result)
	}

	result = HandleMemoryCommand(store, "/mem list")
	if !strings.Contains(result, "No memories") {
		t.Errorf("memory should be empty: %s", result)
	}
}

func TestEnsureBridgeFiles(t *testing.T) {
	dir := t.TempDir()
	EnsureBridgeFiles(dir)

	claudeFile := filepath.Join(dir, "CLAUDE.md")
	data, err := os.ReadFile(claudeFile)
	if err != nil {
		t.Fatalf("CLAUDE.md not created: %v", err)
	}
	if !strings.Contains(string(data), "@AGENTS.md") {
		t.Errorf("CLAUDE.md missing @AGENTS.md reference: %s", string(data))
	}

	geminiFile := filepath.Join(dir, "GEMINI.md")
	data, err = os.ReadFile(geminiFile)
	if err != nil {
		t.Fatalf("GEMINI.md not created: %v", err)
	}
	if !strings.Contains(string(data), "AGENTS.md") {
		t.Errorf("GEMINI.md missing AGENTS.md reference: %s", string(data))
	}

	// Should not overwrite existing files
	os.WriteFile(claudeFile, []byte("custom content"), 0644)
	EnsureBridgeFiles(dir)
	data, _ = os.ReadFile(claudeFile)
	if string(data) != "custom content" {
		t.Error("EnsureBridgeFiles overwrote existing CLAUDE.md")
	}
}

func TestParseAgentActions_Remember(t *testing.T) {
	reply := "好的，我记住了。\n\nACTION:REMEMBER\n项目用 Go 1.22\nEND_ACTION"
	clean, actions := ParseAgentActions(reply)
	if !strings.Contains(clean, "记住了") {
		t.Errorf("clean reply wrong: %s", clean)
	}
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Type != "REMEMBER" {
		t.Errorf("expected REMEMBER, got %s", actions[0].Type)
	}
	if !strings.Contains(actions[0].Body, "Go 1.22") {
		t.Errorf("body wrong: %s", actions[0].Body)
	}
}
