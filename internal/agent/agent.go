package agent

import (
	"os"
	"path/filepath"
	"strings"
)

// AgentConfig describes a single AI coding agent.
//
// The two boolean fields SharedSkillsDir and NativeInstructions are the
// canonical source of truth for agent capability classification. All helper
// functions (UsesSharedSkillsDir, NeedsNoTracking, …) read these fields.
// Set them explicitly when adding a new agent; do not rely on path strings.
//
//	SharedSkillsDir  NativeInstructions  Meaning
//	──────────────── ──────────────────  ────────────────────────────────────────
//	false            false               needs both skills dir + rules symlink
//	true             false               uses shared skills; needs rules symlink
//	false            true                needs skills dir; rules auto-covered
//	true             true                fully automatic; no configuration needed
type AgentConfig struct {
	Name            string
	DisplayName     string
	SkillsDir       string // relative, project-level skills directory path
	GlobalSkillsDir string // absolute, user-level path (empty = global not supported)
	// ExcludeFromPicker, when true, hides this agent from the locked section of
	// the agent picker in `mdm skills add`. Only a small handful of shared-dir
	// agents (replit, universal) are excluded; all others are shown by default.
	ExcludeFromPicker bool

	// InstructionsFile is the project-root path to this agent's instruction
	// file (e.g. "CLAUDE.md", ".cursorrules"). Empty means no instruction file.
	InstructionsFile string

	// SharedSkillsDir is true when this agent reads skills from the shared
	// .agents/skills directory. Skills installed there are available
	// automatically — no per-agent skills directory needs to be configured.
	SharedSkillsDir bool

	// NativeInstructions is true when this agent reads AGENTS.md natively or
	// has no per-project instruction file. When true, no symlink to AGENTS.md
	// is needed and configuredAgents does not need to track this agent for rules.
	NativeInstructions bool

	DetectInstalled func() bool
}

const AgentsDir = ".agents"
const SkillsSubdir = "skills"

func getXDGConfigHome() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config")
}

func getCodexHome() string {
	if dir := strings.TrimSpace(os.Getenv("CODEX_HOME")); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex")
}

func getClaudeHome() string {
	if dir := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

func getOpenClawGlobalSkillsDir() string {
	home, _ := os.UserHomeDir()
	if _, err := os.Stat(filepath.Join(home, ".openclaw")); err == nil {
		return filepath.Join(home, ".openclaw", "skills")
	}
	if _, err := os.Stat(filepath.Join(home, ".clawdbot")); err == nil {
		return filepath.Join(home, ".clawdbot", "skills")
	}
	if _, err := os.Stat(filepath.Join(home, ".moltbot")); err == nil {
		return filepath.Join(home, ".moltbot", "skills")
	}
	return filepath.Join(home, ".openclaw", "skills")
}

func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

var AllAgents map[string]*AgentConfig

func init() {
	home, _ := os.UserHomeDir()
	configHome := getXDGConfigHome()
	codexHome := getCodexHome()
	claudeHome := getClaudeHome()

	// ── SharedSkillsDir=true + NativeInstructions=false ────────────────────────
	// Agents that use .agents/skills but have their own instruction file.
	// Skills are auto-covered; only the rules symlink needs to be configured.

	sharedSkillsUniqueRules := map[string]*AgentConfig{
		"amp": {
			Name:               "amp",
			DisplayName:        "Amp",
			SkillsDir:          ".agents/skills",
			GlobalSkillsDir:    filepath.Join(configHome, "agents/skills"),
			InstructionsFile:   "AMP.md",
			SharedSkillsDir:    true,
			NativeInstructions: false,
			DetectInstalled:    func() bool { return pathExists(filepath.Join(configHome, "amp")) },
		},
		"cline": {
			Name:               "cline",
			DisplayName:        "Cline",
			SkillsDir:          ".agents/skills",
			GlobalSkillsDir:    filepath.Join(home, ".agents/skills"),
			InstructionsFile:   ".clinerules",
			SharedSkillsDir:    true,
			NativeInstructions: false,
			DetectInstalled:    func() bool { return pathExists(filepath.Join(home, ".cline")) },
		},
		"cursor": {
			Name:               "cursor",
			DisplayName:        "Cursor",
			SkillsDir:          ".agents/skills",
			GlobalSkillsDir:    filepath.Join(home, ".cursor/skills"),
			InstructionsFile:   ".cursorrules",
			SharedSkillsDir:    true,
			NativeInstructions: false,
			DetectInstalled:    func() bool { return pathExists(filepath.Join(home, ".cursor")) },
		},
		"gemini-cli": {
			Name:               "gemini-cli",
			DisplayName:        "Gemini CLI",
			SkillsDir:          ".agents/skills",
			GlobalSkillsDir:    filepath.Join(home, ".gemini/skills"),
			InstructionsFile:   "GEMINI.md",
			SharedSkillsDir:    true,
			NativeInstructions: false,
			DetectInstalled:    func() bool { return pathExists(filepath.Join(home, ".gemini")) },
		},
		"github-copilot": {
			Name:               "github-copilot",
			DisplayName:        "GitHub Copilot",
			SkillsDir:          ".agents/skills",
			GlobalSkillsDir:    filepath.Join(home, ".copilot/skills"),
			InstructionsFile:   ".github/copilot-instructions.md",
			SharedSkillsDir:    true,
			NativeInstructions: false,
			DetectInstalled:    func() bool { return pathExists(filepath.Join(home, ".copilot")) },
		},
	}

	// ── SharedSkillsDir=true + NativeInstructions=true ─────────────────────────
	// Fully automatic agents: skills come from .agents/skills and instructions
	// are read from AGENTS.md (or this agent has no instruction file). No
	// configuration in configuredAgents is needed for these agents.

	fullyAutomatic := map[string]*AgentConfig{
		"antigravity": {
			Name:               "antigravity",
			DisplayName:        "Antigravity",
			SkillsDir:          ".agents/skills",
			GlobalSkillsDir:    filepath.Join(home, ".gemini/antigravity/skills"),
			SharedSkillsDir:    true,
			NativeInstructions: true,
			DetectInstalled:    func() bool { return pathExists(filepath.Join(home, ".gemini/antigravity")) },
		},
		"codex": {
			Name:               "codex",
			DisplayName:        "Codex",
			SkillsDir:          ".agents/skills",
			GlobalSkillsDir:    filepath.Join(codexHome, "skills"),
			InstructionsFile:   "AGENTS.md",
			SharedSkillsDir:    true,
			NativeInstructions: true,
			DetectInstalled:    func() bool { return pathExists(codexHome) || pathExists("/etc/codex") },
		},
		"deepagents": {
			Name:               "deepagents",
			DisplayName:        "Deep Agents",
			SkillsDir:          ".agents/skills",
			GlobalSkillsDir:    filepath.Join(home, ".deepagents/agent/skills"),
			SharedSkillsDir:    true,
			NativeInstructions: true,
			DetectInstalled:    func() bool { return pathExists(filepath.Join(home, ".deepagents")) },
		},
		"firebender": {
			Name:               "firebender",
			DisplayName:        "Firebender",
			SkillsDir:          ".agents/skills",
			GlobalSkillsDir:    filepath.Join(home, ".firebender/skills"),
			SharedSkillsDir:    true,
			NativeInstructions: true,
			DetectInstalled:    func() bool { return pathExists(filepath.Join(home, ".firebender")) },
		},
		"kimi-cli": {
			Name:               "kimi-cli",
			DisplayName:        "Kimi Code CLI",
			SkillsDir:          ".agents/skills",
			GlobalSkillsDir:    filepath.Join(home, ".config/agents/skills"),
			SharedSkillsDir:    true,
			NativeInstructions: true,
			DetectInstalled:    func() bool { return pathExists(filepath.Join(home, ".kimi")) },
		},
		"opencode": {
			Name:               "opencode",
			DisplayName:        "OpenCode",
			SkillsDir:          ".agents/skills",
			GlobalSkillsDir:    filepath.Join(configHome, "opencode/skills"),
			InstructionsFile:   "AGENTS.md",
			SharedSkillsDir:    true,
			NativeInstructions: true,
			DetectInstalled:    func() bool { return pathExists(filepath.Join(configHome, "opencode")) },
		},
		"replit": {
			Name:               "replit",
			DisplayName:        "Replit",
			SkillsDir:          ".agents/skills",
			GlobalSkillsDir:    filepath.Join(configHome, "agents/skills"),
			ExcludeFromPicker:  true,
			InstructionsFile:   "AGENTS.md",
			SharedSkillsDir:    true,
			NativeInstructions: true,
			DetectInstalled: func() bool {
				cwd, _ := os.Getwd()
				return pathExists(filepath.Join(cwd, ".replit"))
			},
		},
		"universal": {
			Name:               "universal",
			DisplayName:        "Universal",
			SkillsDir:          ".agents/skills",
			GlobalSkillsDir:    filepath.Join(configHome, "agents/skills"),
			ExcludeFromPicker:  true,
			SharedSkillsDir:    true,
			NativeInstructions: true,
			DetectInstalled:    func() bool { return false },
		},
		"warp": {
			Name:               "warp",
			DisplayName:        "Warp",
			SkillsDir:          ".agents/skills",
			GlobalSkillsDir:    filepath.Join(home, ".agents/skills"),
			SharedSkillsDir:    true,
			NativeInstructions: true,
			DetectInstalled:    func() bool { return pathExists(filepath.Join(home, ".warp")) },
		},
	}

	// ── SharedSkillsDir=false + NativeInstructions=false ───────────────────────
	// Agents that need both a dedicated skills directory AND a rules symlink.
	// Both must be explicitly configured via configuredAgents.

	uniqueSkillsUniqueRules := map[string]*AgentConfig{
		"claude-code": {
			Name:               "claude-code",
			DisplayName:        "Claude Code",
			SkillsDir:          ".claude/skills",
			GlobalSkillsDir:    filepath.Join(claudeHome, "skills"),
			InstructionsFile:   "CLAUDE.md",
			SharedSkillsDir:    false,
			NativeInstructions: false,
			DetectInstalled:    func() bool { return pathExists(claudeHome) },
		},
		"roo": {
			Name:               "roo",
			DisplayName:        "Roo Code",
			SkillsDir:          ".roo/skills",
			GlobalSkillsDir:    filepath.Join(home, ".roo/skills"),
			InstructionsFile:   ".roorules",
			SharedSkillsDir:    false,
			NativeInstructions: false,
			DetectInstalled:    func() bool { return pathExists(filepath.Join(home, ".roo")) },
		},
		"windsurf": {
			Name:               "windsurf",
			DisplayName:        "Windsurf",
			SkillsDir:          ".windsurf/skills",
			GlobalSkillsDir:    filepath.Join(home, ".codeium/windsurf/skills"),
			InstructionsFile:   ".windsurfrules",
			SharedSkillsDir:    false,
			NativeInstructions: false,
			DetectInstalled:    func() bool { return pathExists(filepath.Join(home, ".codeium/windsurf")) },
		},
	}

	// ── SharedSkillsDir=false + NativeInstructions=true ────────────────────────
	// Agents with their own dedicated skills directory but no per-project
	// instruction file (or they read AGENTS.md natively). Only the skills
	// directory needs to be configured via configuredAgents.

	uniqueSkillsNativeRules := map[string]*AgentConfig{
		"adal": {
			Name:               "adal",
			DisplayName:        "AdaL",
			SkillsDir:          ".adal/skills",
			GlobalSkillsDir:    filepath.Join(home, ".adal/skills"),
			SharedSkillsDir:    false,
			NativeInstructions: true,
			DetectInstalled:    func() bool { return pathExists(filepath.Join(home, ".adal")) },
		},
		"augment": {
			Name:               "augment",
			DisplayName:        "Augment",
			SkillsDir:          ".augment/skills",
			GlobalSkillsDir:    filepath.Join(home, ".augment/skills"),
			SharedSkillsDir:    false,
			NativeInstructions: true,
			DetectInstalled:    func() bool { return pathExists(filepath.Join(home, ".augment")) },
		},
		"bob": {
			Name:               "bob",
			DisplayName:        "IBM Bob",
			SkillsDir:          ".bob/skills",
			GlobalSkillsDir:    filepath.Join(home, ".bob/skills"),
			SharedSkillsDir:    false,
			NativeInstructions: true,
			DetectInstalled:    func() bool { return pathExists(filepath.Join(home, ".bob")) },
		},
		"codebuddy": {
			Name:               "codebuddy",
			DisplayName:        "CodeBuddy",
			SkillsDir:          ".codebuddy/skills",
			GlobalSkillsDir:    filepath.Join(home, ".codebuddy/skills"),
			SharedSkillsDir:    false,
			NativeInstructions: true,
			DetectInstalled: func() bool {
				cwd, _ := os.Getwd()
				return pathExists(filepath.Join(cwd, ".codebuddy")) || pathExists(filepath.Join(home, ".codebuddy"))
			},
		},
		"command-code": {
			Name:               "command-code",
			DisplayName:        "Command Code",
			SkillsDir:          ".commandcode/skills",
			GlobalSkillsDir:    filepath.Join(home, ".commandcode/skills"),
			SharedSkillsDir:    false,
			NativeInstructions: true,
			DetectInstalled:    func() bool { return pathExists(filepath.Join(home, ".commandcode")) },
		},
		"continue": {
			Name:               "continue",
			DisplayName:        "Continue",
			SkillsDir:          ".continue/skills",
			GlobalSkillsDir:    filepath.Join(home, ".continue/skills"),
			SharedSkillsDir:    false,
			NativeInstructions: true,
			DetectInstalled: func() bool {
				cwd, _ := os.Getwd()
				return pathExists(filepath.Join(cwd, ".continue")) || pathExists(filepath.Join(home, ".continue"))
			},
		},
		"cortex": {
			Name:               "cortex",
			DisplayName:        "Cortex Code",
			SkillsDir:          ".cortex/skills",
			GlobalSkillsDir:    filepath.Join(home, ".snowflake/cortex/skills"),
			SharedSkillsDir:    false,
			NativeInstructions: true,
			DetectInstalled:    func() bool { return pathExists(filepath.Join(home, ".snowflake/cortex")) },
		},
		"crush": {
			Name:               "crush",
			DisplayName:        "Crush",
			SkillsDir:          ".crush/skills",
			GlobalSkillsDir:    filepath.Join(home, ".config/crush/skills"),
			SharedSkillsDir:    false,
			NativeInstructions: true,
			DetectInstalled:    func() bool { return pathExists(filepath.Join(home, ".config/crush")) },
		},
		"droid": {
			Name:               "droid",
			DisplayName:        "Droid",
			SkillsDir:          ".factory/skills",
			GlobalSkillsDir:    filepath.Join(home, ".factory/skills"),
			SharedSkillsDir:    false,
			NativeInstructions: true,
			DetectInstalled:    func() bool { return pathExists(filepath.Join(home, ".factory")) },
		},
		"goose": {
			Name:               "goose",
			DisplayName:        "Goose",
			SkillsDir:          ".goose/skills",
			GlobalSkillsDir:    filepath.Join(configHome, "goose/skills"),
			SharedSkillsDir:    false,
			NativeInstructions: true,
			DetectInstalled:    func() bool { return pathExists(filepath.Join(configHome, "goose")) },
		},
		"iflow-cli": {
			Name:               "iflow-cli",
			DisplayName:        "iFlow CLI",
			SkillsDir:          ".iflow/skills",
			GlobalSkillsDir:    filepath.Join(home, ".iflow/skills"),
			SharedSkillsDir:    false,
			NativeInstructions: true,
			DetectInstalled:    func() bool { return pathExists(filepath.Join(home, ".iflow")) },
		},
		"junie": {
			Name:               "junie",
			DisplayName:        "Junie",
			SkillsDir:          ".junie/skills",
			GlobalSkillsDir:    filepath.Join(home, ".junie/skills"),
			SharedSkillsDir:    false,
			NativeInstructions: true,
			DetectInstalled:    func() bool { return pathExists(filepath.Join(home, ".junie")) },
		},
		"kilo": {
			Name:               "kilo",
			DisplayName:        "Kilo Code",
			SkillsDir:          ".kilocode/skills",
			GlobalSkillsDir:    filepath.Join(home, ".kilocode/skills"),
			SharedSkillsDir:    false,
			NativeInstructions: true,
			DetectInstalled:    func() bool { return pathExists(filepath.Join(home, ".kilocode")) },
		},
		"kiro-cli": {
			Name:               "kiro-cli",
			DisplayName:        "Kiro CLI",
			SkillsDir:          ".kiro/skills",
			GlobalSkillsDir:    filepath.Join(home, ".kiro/skills"),
			SharedSkillsDir:    false,
			NativeInstructions: true,
			DetectInstalled:    func() bool { return pathExists(filepath.Join(home, ".kiro")) },
		},
		"kode": {
			Name:               "kode",
			DisplayName:        "Kode",
			SkillsDir:          ".kode/skills",
			GlobalSkillsDir:    filepath.Join(home, ".kode/skills"),
			SharedSkillsDir:    false,
			NativeInstructions: true,
			DetectInstalled:    func() bool { return pathExists(filepath.Join(home, ".kode")) },
		},
		"mcpjam": {
			Name:               "mcpjam",
			DisplayName:        "MCPJam",
			SkillsDir:          ".mcpjam/skills",
			GlobalSkillsDir:    filepath.Join(home, ".mcpjam/skills"),
			SharedSkillsDir:    false,
			NativeInstructions: true,
			DetectInstalled:    func() bool { return pathExists(filepath.Join(home, ".mcpjam")) },
		},
		"mistral-vibe": {
			Name:               "mistral-vibe",
			DisplayName:        "Mistral Vibe",
			SkillsDir:          ".vibe/skills",
			GlobalSkillsDir:    filepath.Join(home, ".vibe/skills"),
			SharedSkillsDir:    false,
			NativeInstructions: true,
			DetectInstalled:    func() bool { return pathExists(filepath.Join(home, ".vibe")) },
		},
		"mux": {
			Name:               "mux",
			DisplayName:        "Mux",
			SkillsDir:          ".mux/skills",
			GlobalSkillsDir:    filepath.Join(home, ".mux/skills"),
			SharedSkillsDir:    false,
			NativeInstructions: true,
			DetectInstalled:    func() bool { return pathExists(filepath.Join(home, ".mux")) },
		},
		"neovate": {
			Name:               "neovate",
			DisplayName:        "Neovate",
			SkillsDir:          ".neovate/skills",
			GlobalSkillsDir:    filepath.Join(home, ".neovate/skills"),
			SharedSkillsDir:    false,
			NativeInstructions: true,
			DetectInstalled:    func() bool { return pathExists(filepath.Join(home, ".neovate")) },
		},
		"openclaw": {
			Name:               "openclaw",
			DisplayName:        "OpenClaw",
			SkillsDir:          "skills",
			GlobalSkillsDir:    getOpenClawGlobalSkillsDir(),
			SharedSkillsDir:    false,
			NativeInstructions: true,
			DetectInstalled: func() bool {
				return pathExists(filepath.Join(home, ".openclaw")) ||
					pathExists(filepath.Join(home, ".clawdbot")) ||
					pathExists(filepath.Join(home, ".moltbot"))
			},
		},
		"openhands": {
			Name:               "openhands",
			DisplayName:        "OpenHands",
			SkillsDir:          ".openhands/skills",
			GlobalSkillsDir:    filepath.Join(home, ".openhands/skills"),
			SharedSkillsDir:    false,
			NativeInstructions: true,
			DetectInstalled:    func() bool { return pathExists(filepath.Join(home, ".openhands")) },
		},
		"pi": {
			Name:               "pi",
			DisplayName:        "Pi",
			SkillsDir:          ".pi/skills",
			GlobalSkillsDir:    filepath.Join(home, ".pi/agent/skills"),
			SharedSkillsDir:    false,
			NativeInstructions: true,
			DetectInstalled:    func() bool { return pathExists(filepath.Join(home, ".pi/agent")) },
		},
		"pochi": {
			Name:               "pochi",
			DisplayName:        "Pochi",
			SkillsDir:          ".pochi/skills",
			GlobalSkillsDir:    filepath.Join(home, ".pochi/skills"),
			SharedSkillsDir:    false,
			NativeInstructions: true,
			DetectInstalled:    func() bool { return pathExists(filepath.Join(home, ".pochi")) },
		},
		"qoder": {
			Name:               "qoder",
			DisplayName:        "Qoder",
			SkillsDir:          ".qoder/skills",
			GlobalSkillsDir:    filepath.Join(home, ".qoder/skills"),
			SharedSkillsDir:    false,
			NativeInstructions: true,
			DetectInstalled:    func() bool { return pathExists(filepath.Join(home, ".qoder")) },
		},
		"qwen-code": {
			Name:               "qwen-code",
			DisplayName:        "Qwen Code",
			SkillsDir:          ".qwen/skills",
			GlobalSkillsDir:    filepath.Join(home, ".qwen/skills"),
			SharedSkillsDir:    false,
			NativeInstructions: true,
			DetectInstalled:    func() bool { return pathExists(filepath.Join(home, ".qwen")) },
		},
		"trae": {
			Name:               "trae",
			DisplayName:        "Trae",
			SkillsDir:          ".trae/skills",
			GlobalSkillsDir:    filepath.Join(home, ".trae/skills"),
			SharedSkillsDir:    false,
			NativeInstructions: true,
			DetectInstalled:    func() bool { return pathExists(filepath.Join(home, ".trae")) },
		},
		"trae-cn": {
			Name:               "trae-cn",
			DisplayName:        "Trae CN",
			SkillsDir:          ".trae/skills",
			GlobalSkillsDir:    filepath.Join(home, ".trae-cn/skills"),
			SharedSkillsDir:    false,
			NativeInstructions: true,
			DetectInstalled:    func() bool { return pathExists(filepath.Join(home, ".trae-cn")) },
		},
		"zencoder": {
			Name:               "zencoder",
			DisplayName:        "Zencoder",
			SkillsDir:          ".zencoder/skills",
			GlobalSkillsDir:    filepath.Join(home, ".zencoder/skills"),
			SharedSkillsDir:    false,
			NativeInstructions: true,
			DetectInstalled:    func() bool { return pathExists(filepath.Join(home, ".zencoder")) },
		},
	}

	AllAgents = make(map[string]*AgentConfig)
	for k, v := range sharedSkillsUniqueRules {
		AllAgents[k] = v
	}
	for k, v := range fullyAutomatic {
		AllAgents[k] = v
	}
	for k, v := range uniqueSkillsUniqueRules {
		AllAgents[k] = v
	}
	for k, v := range uniqueSkillsNativeRules {
		AllAgents[k] = v
	}
}

func DetectInstalledAgents() []string {
	var installed []string
	for name, a := range AllAgents {
		if a.DetectInstalled() {
			installed = append(installed, name)
		}
	}
	return installed
}

// ─── Agent classification helpers ─────────────────────────────────────────────
//
// These functions read the explicit SharedSkillsDir and NativeInstructions
// boolean fields on AgentConfig. Do not add new string comparisons against
// SkillsDir or InstructionsFile in calling code — use these helpers instead.

// UsesSharedSkillsDir reports whether the agent reads skills from the shared
// .agents/skills directory (SharedSkillsDir == true).
func UsesSharedSkillsDir(name string) bool {
	a, ok := AllAgents[name]
	return ok && a.SharedSkillsDir
}

// NeedsNoTracking reports whether an agent requires no entry in configuredAgents.
// True when both skills and instructions are auto-covered
// (SharedSkillsDir && NativeInstructions).
func NeedsNoTracking(name string) bool {
	a, ok := AllAgents[name]
	return ok && a.SharedSkillsDir && a.NativeInstructions
}

// GetSharedSkillsDirAgents returns agents that use .agents/skills and are
// shown in the locked section of the agent picker (ExcludeFromPicker == false).
func GetSharedSkillsDirAgents() []string {
	var result []string
	for name, a := range AllAgents {
		if a.SharedSkillsDir && !a.ExcludeFromPicker {
			result = append(result, name)
		}
	}
	return result
}

// GetUniqueSkillsDirAgents returns agents with their own dedicated skills
// directory. These always need explicit configuration.
func GetUniqueSkillsDirAgents() []string {
	var result []string
	for name, a := range AllAgents {
		if !a.SharedSkillsDir {
			result = append(result, name)
		}
	}
	return result
}
