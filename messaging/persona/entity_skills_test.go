package persona

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestStoreWithSkills returns a Store whose ResolvedConfig has both
// MemoryDir and SkillsDir set to subdirs of a fresh temp dir. The
// caller owns the temp dir (cleaned up via t.TempDir()).
func newTestStoreWithSkills(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := ResolvedConfig{
		Enabled:              true,
		SoulFile:             filepath.Join(dir, "SOUL.md"),
		MemoryDir:            filepath.Join(dir, "memory"),
		SkillsDir:            filepath.Join(dir, "skills"),
		MaxSoulChars:         500,
		MaxChatMemoryChars:   500,
		MaxUserMemoryChars:   500,
		MaxGlobalMemoryChars: 500,
		MaxEntityMemoryChars: 8000,
	}
	return NewStore(cfg), dir
}

// ---- Entity memory tests ----

func TestStore_EntityMemory_RoundTrip(t *testing.T) {
	s, _ := newTestStoreWithSkills(t)

	const entityID = "customer-001"
	const content = "VIP customer, prefers callbacks on Mondays."

	if err := s.SaveEntity(entityID, content); err != nil {
		t.Fatalf("SaveEntity: %v", err)
	}

	got, err := s.LoadEntity(entityID)
	if err != nil {
		t.Fatalf("LoadEntity: %v", err)
	}
	if got != content {
		t.Errorf("LoadEntity = %q, want %q", got, content)
	}
}

func TestStore_EntityMemory_Missing(t *testing.T) {
	s, _ := newTestStoreWithSkills(t)

	got, err := s.LoadEntity("nonexistent-entity")
	if err != nil {
		t.Fatalf("LoadEntity on missing file should not error, got: %v", err)
	}
	if got != "" {
		t.Errorf("LoadEntity on missing file should return empty, got %q", got)
	}
}

func TestStore_EntityMemory_Clear(t *testing.T) {
	s, _ := newTestStoreWithSkills(t)

	const entityID = "entity-42"

	if err := s.SaveEntity(entityID, "some content"); err != nil {
		t.Fatalf("SaveEntity: %v", err)
	}

	if err := s.ClearEntity(entityID); err != nil {
		t.Fatalf("ClearEntity: %v", err)
	}

	got, err := s.LoadEntity(entityID)
	if err != nil {
		t.Fatalf("LoadEntity after clear: %v", err)
	}
	if got != "" {
		t.Errorf("after ClearEntity expected empty, got %q", got)
	}

	// Clearing an already-absent entity must be a no-op, not an error.
	if err := s.ClearEntity(entityID); err != nil {
		t.Errorf("second ClearEntity: %v", err)
	}
}

func TestStore_EntityMemory_SaveReplaces(t *testing.T) {
	s, _ := newTestStoreWithSkills(t)

	const entityID = "entity-99"

	if err := s.SaveEntity(entityID, "first version"); err != nil {
		t.Fatalf("first SaveEntity: %v", err)
	}
	if err := s.SaveEntity(entityID, "second version"); err != nil {
		t.Fatalf("second SaveEntity: %v", err)
	}

	got, err := s.LoadEntity(entityID)
	if err != nil {
		t.Fatalf("LoadEntity: %v", err)
	}
	if strings.Contains(got, "first version") {
		t.Errorf("SaveEntity should replace, not append; got %q", got)
	}
	if !strings.Contains(got, "second version") {
		t.Errorf("SaveEntity result missing new content; got %q", got)
	}
}

func TestStore_EntityMemory_TruncatesToCap(t *testing.T) {
	dir := t.TempDir()
	cfg := ResolvedConfig{
		Enabled:              true,
		SoulFile:             filepath.Join(dir, "SOUL.md"),
		MemoryDir:            filepath.Join(dir, "memory"),
		SkillsDir:            filepath.Join(dir, "skills"),
		MaxEntityMemoryChars: 50,
	}
	s := NewStore(cfg)

	longContent := strings.Repeat("Z", 200)
	if err := s.SaveEntity("e1", longContent); err != nil {
		t.Fatalf("SaveEntity: %v", err)
	}

	got, err := s.LoadEntity("e1")
	if err != nil {
		t.Fatalf("LoadEntity: %v", err)
	}
	if len(got) > 50 {
		t.Errorf("LoadEntity len %d exceeds cap 50", len(got))
	}
}

// ---- Skills index tests ----

func TestStore_LoadSkillsIndex_Empty(t *testing.T) {
	s, _ := newTestStoreWithSkills(t)

	entries, err := s.LoadSkillsIndex()
	if err != nil {
		t.Fatalf("LoadSkillsIndex on empty dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestStore_LoadSkillsIndex_MissingDir(t *testing.T) {
	dir := t.TempDir()
	cfg := ResolvedConfig{
		Enabled:   true,
		SkillsDir: filepath.Join(dir, "nonexistent-skills"),
	}
	s := NewStore(cfg)

	entries, err := s.LoadSkillsIndex()
	if err != nil {
		t.Fatalf("LoadSkillsIndex with missing dir should not error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries for missing dir, got %d", len(entries))
	}
}

func TestStore_LoadSkillsIndex_MultipleSkills(t *testing.T) {
	s, dir := newTestStoreWithSkills(t)
	skillsDir := filepath.Join(dir, "skills")

	// Create two skills with proper SKILL.md files.
	skills := []struct {
		name    string
		content string
	}{
		{
			name: "dispatch-confirm",
			content: "# dispatch-confirm\n派单确认工作流\n\nFull description of the dispatch-confirm skill.\n",
		},
		{
			name: "complaint-detection",
			content: "# complaint-detection\n投诉检测和升级\n\nMore details here.\n",
		},
	}

	for _, sk := range skills {
		skDir := filepath.Join(skillsDir, sk.name)
		if err := os.MkdirAll(skDir, 0o700); err != nil {
			t.Fatalf("create skill dir %s: %v", sk.name, err)
		}
		if err := os.WriteFile(filepath.Join(skDir, "SKILL.md"), []byte(sk.content), 0o600); err != nil {
			t.Fatalf("write SKILL.md for %s: %v", sk.name, err)
		}
	}

	entries, err := s.LoadSkillsIndex()
	if err != nil {
		t.Fatalf("LoadSkillsIndex: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	// Results must be sorted by name.
	if entries[0].Name != "complaint-detection" {
		t.Errorf("entries[0].Name = %q, want %q", entries[0].Name, "complaint-detection")
	}
	if entries[1].Name != "dispatch-confirm" {
		t.Errorf("entries[1].Name = %q, want %q", entries[1].Name, "dispatch-confirm")
	}

	// Descriptions come from second non-empty line.
	if !strings.Contains(entries[1].Description, "派单确认工作流") {
		t.Errorf("dispatch-confirm description = %q, want to contain %q", entries[1].Description, "派单确认工作流")
	}
	if !strings.Contains(entries[0].Description, "投诉检测和升级") {
		t.Errorf("complaint-detection description = %q, want to contain %q", entries[0].Description, "投诉检测和升级")
	}
}

func TestStore_LoadSkillsIndex_SkipsSubdirsWithoutSkillMD(t *testing.T) {
	s, dir := newTestStoreWithSkills(t)
	skillsDir := filepath.Join(dir, "skills")

	// Create a skill with SKILL.md.
	goodDir := filepath.Join(skillsDir, "good-skill")
	if err := os.MkdirAll(goodDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(goodDir, "SKILL.md"), []byte("# good-skill\nA good skill.\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Create a directory without SKILL.md.
	badDir := filepath.Join(skillsDir, "no-skill-file")
	if err := os.MkdirAll(badDir, 0o700); err != nil {
		t.Fatal(err)
	}

	// Create a regular file (should be ignored, only subdirs matter).
	if err := os.WriteFile(filepath.Join(skillsDir, "README.md"), []byte("ignore me"), 0o600); err != nil {
		t.Fatal(err)
	}

	entries, err := s.LoadSkillsIndex()
	if err != nil {
		t.Fatalf("LoadSkillsIndex: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry (only 'good-skill'), got %d: %v", len(entries), entries)
	}
	if entries[0].Name != "good-skill" {
		t.Errorf("entry name = %q, want %q", entries[0].Name, "good-skill")
	}
}

// ---- LoadSkill tests ----

func TestStore_LoadSkill_Found(t *testing.T) {
	s, dir := newTestStoreWithSkills(t)
	skillsDir := filepath.Join(dir, "skills")

	const skillName = "my-skill"
	const skillContent = "# my-skill\nDescription line.\n\nFull skill body here."

	skDir := filepath.Join(skillsDir, skillName)
	if err := os.MkdirAll(skDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skDir, "SKILL.md"), []byte(skillContent), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := s.LoadSkill(skillName)
	if err != nil {
		t.Fatalf("LoadSkill: %v", err)
	}
	if got != skillContent {
		t.Errorf("LoadSkill = %q, want %q", got, skillContent)
	}
}

func TestStore_LoadSkill_NotFound(t *testing.T) {
	s, _ := newTestStoreWithSkills(t)

	got, err := s.LoadSkill("nonexistent-skill")
	if err != nil {
		t.Fatalf("LoadSkill for missing skill should not error: %v", err)
	}
	if got != "" {
		t.Errorf("LoadSkill for missing skill should return empty, got %q", got)
	}
}

// ---- BuildWithEntity tests ----

func TestLoader_BuildWithEntity_InjectsEntity(t *testing.T) {
	s, _ := newTestStoreWithSkills(t)
	l := NewLoader(s)

	const entityID = "customer-XYZ"
	const entityContent = "Customer XYZ: prefers email, tier=gold."

	if err := s.SaveEntity(entityID, entityContent); err != nil {
		t.Fatalf("SaveEntity: %v", err)
	}

	got := l.BuildWithEntity(context.Background(), "chat-1", "user-1", false, entityID)

	if !strings.Contains(got, `<context type="memory" scope="entity">`) {
		t.Errorf("banner missing entity memory section:\n%s", got)
	}
	if !strings.Contains(got, entityContent) {
		t.Errorf("banner missing entity content:\n%s", got)
	}
}

func TestLoader_BuildWithEntity_EmptyEntityID_NoEntitySection(t *testing.T) {
	s, _ := newTestStoreWithSkills(t)
	l := NewLoader(s)

	// No soul, no memory, no entity ID — should produce empty banner.
	got := l.BuildWithEntity(context.Background(), "chat-1", "user-1", false, "")
	if got != "" {
		t.Errorf("BuildWithEntity with empty entityID should produce empty banner when no content, got %q", got)
	}
}

func TestLoader_BuildWithEntity_MissingEntity_OmitsSection(t *testing.T) {
	s, _ := newTestStoreWithSkills(t)
	l := NewLoader(s)

	// Entity ID provided but no file on disk — should silently omit the section.
	got := l.BuildWithEntity(context.Background(), "chat-1", "user-1", false, "ghost-entity")
	if strings.Contains(got, `scope="entity"`) {
		t.Errorf("banner should omit entity section when entity file is missing:\n%s", got)
	}
}

func TestLoader_BuildWithEntity_IncludesOtherSections(t *testing.T) {
	s, _ := newTestStoreWithSkills(t)
	l := NewLoader(s)

	_ = s.EnsureSoulTemplate()
	_ = s.AppendMemory(ScopeChat, "chat-1", "chat fact")
	_ = s.SaveEntity("ent-1", "entity detail")

	got := l.BuildWithEntity(context.Background(), "chat-1", "user-1", false, "ent-1")

	if !strings.Contains(got, `<context type="persona">`) {
		t.Errorf("BuildWithEntity missing persona section:\n%s", got)
	}
	if !strings.Contains(got, `scope="chat"`) {
		t.Errorf("BuildWithEntity missing chat memory section:\n%s", got)
	}
	if !strings.Contains(got, `scope="entity"`) {
		t.Errorf("BuildWithEntity missing entity section:\n%s", got)
	}
}

// ---- Build() skills index injection tests ----

func TestLoader_Build_InjectsSkillsIndex(t *testing.T) {
	s, dir := newTestStoreWithSkills(t)
	l := NewLoader(s)

	skillsDir := filepath.Join(dir, "skills")

	// Create one skill.
	skDir := filepath.Join(skillsDir, "dispatch-confirm")
	if err := os.MkdirAll(skDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(skDir, "SKILL.md"),
		[]byte("# dispatch-confirm\n派单确认工作流\n\nDetails.\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	// Need soul so the banner is non-empty.
	_ = s.EnsureSoulTemplate()

	got := l.Build(context.Background(), "chat-1", "user-1", false)

	if !strings.Contains(got, `<context type="skills">`) {
		t.Errorf("banner missing skills context section:\n%s", got)
	}
	if !strings.Contains(got, "dispatch-confirm") {
		t.Errorf("banner missing skill name:\n%s", got)
	}
}

func TestLoader_Build_NoSkillsIndex_WhenDirEmpty(t *testing.T) {
	s, _ := newTestStoreWithSkills(t)
	l := NewLoader(s)

	_ = s.EnsureSoulTemplate()

	got := l.Build(context.Background(), "chat-1", "user-1", false)

	if strings.Contains(got, `<context type="skills">`) {
		t.Errorf("banner should not have skills section when skills dir is empty:\n%s", got)
	}
}
