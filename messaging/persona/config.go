package persona

import (
	"os"
	"path/filepath"
	"strings"
)

// Config mirrors the config.json "persona" block. Zero values mean
// "use the default"; Resolved() turns this into a ready-to-use
// ResolvedConfig with absolute paths and populated limits.
//
// Enabled is a pointer so an explicit `false` in config.json can
// disable the feature even after we start shipping defaults that
// would otherwise turn it on. nil means "use default (true)".
type Config struct {
	Enabled              *bool  `json:"enabled,omitempty"`
	SoulFile             string `json:"soul_file,omitempty"`
	MemoryDir            string `json:"memory_dir,omitempty"`
	SkillsDir            string `json:"skills_dir,omitempty"`
	MaxSoulChars         int    `json:"max_soul_chars,omitempty"`
	MaxChatMemoryChars   int    `json:"max_chat_memory_chars,omitempty"`
	MaxUserMemoryChars   int    `json:"max_user_memory_chars,omitempty"`
	MaxGlobalMemoryChars int    `json:"max_global_memory_chars,omitempty"`
	MaxEntityMemoryChars int    `json:"max_entity_memory_chars,omitempty"`
}

// IsEnabled reports whether the persona machinery should run. Defaults
// to true when the field is unset in config.json so a fresh install
// gets the feature without an explicit toggle.
func (c Config) IsEnabled() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

// Defaults used when the user leaves fields blank. The numbers are
// deliberately conservative — ~2.5 KB of banner text per turn keeps
// the token overhead modest across common chat volumes.
const (
	DefaultMaxSoulChars         = 2000
	DefaultMaxChatMemoryChars   = 4000
	DefaultMaxUserMemoryChars   = 2000
	DefaultMaxGlobalMemoryChars = 2000
	DefaultMaxEntityMemoryChars = 8000
)

// ResolvedConfig is Config with all optional fields populated from
// defaults and tilde-expansion applied to paths. Construct one by
// calling Config.Resolved().
type ResolvedConfig struct {
	Enabled              bool
	SoulFile             string
	MemoryDir            string
	SkillsDir            string
	MaxSoulChars         int
	MaxChatMemoryChars   int
	MaxUserMemoryChars   int
	MaxGlobalMemoryChars int
	MaxEntityMemoryChars int
}

// Resolved fills in defaults and expands "~" prefixes to the user's
// home directory. Returns a ResolvedConfig whose paths are ready to
// use with os.Open / os.WriteFile.
//
// A failed home lookup (very rare on real systems) leaves "~" in
// place; the caller should treat Enabled=false as the safe fallback
// when the downstream open attempt fails, rather than silently
// writing to an unexpected path.
func (c Config) Resolved() ResolvedConfig {
	home, _ := os.UserHomeDir()
	soul := strings.TrimSpace(c.SoulFile)
	if soul == "" {
		soul = filepath.Join(home, ".ringclaw", "SOUL.md")
	}
	memDir := strings.TrimSpace(c.MemoryDir)
	if memDir == "" {
		memDir = filepath.Join(home, ".ringclaw", "memory")
	}
	skillsDir := strings.TrimSpace(c.SkillsDir)
	if skillsDir == "" {
		skillsDir = filepath.Join(home, ".ringclaw", "skills")
	}
	return ResolvedConfig{
		Enabled:              c.IsEnabled(),
		SoulFile:             expandHome(soul, home),
		MemoryDir:            expandHome(memDir, home),
		SkillsDir:            expandHome(skillsDir, home),
		MaxSoulChars:         pickInt(c.MaxSoulChars, DefaultMaxSoulChars),
		MaxChatMemoryChars:   pickInt(c.MaxChatMemoryChars, DefaultMaxChatMemoryChars),
		MaxUserMemoryChars:   pickInt(c.MaxUserMemoryChars, DefaultMaxUserMemoryChars),
		MaxGlobalMemoryChars: pickInt(c.MaxGlobalMemoryChars, DefaultMaxGlobalMemoryChars),
		MaxEntityMemoryChars: pickInt(c.MaxEntityMemoryChars, DefaultMaxEntityMemoryChars),
	}
}

func expandHome(path, home string) string {
	if home == "" || path == "" {
		return path
	}
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}

func pickInt(got, fallback int) int {
	if got > 0 {
		return got
	}
	return fallback
}
