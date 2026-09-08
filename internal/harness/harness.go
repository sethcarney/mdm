package harness

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/sethcarney/mdm/internal/agentfile"
)

// HarnessConfig describes a single AI coding harness. SharedSkillsDir and
// NativeInstructions classify it, and every helper (UsesSharedSkillsDir,
// NeedsNoTracking) reads those two fields. Set them explicitly for a new
// harness; do not infer from path strings. SharedSkillsDir=false needs a skills
// directory, NativeInstructions=false needs a rules symlink.
type HarnessConfig struct {
	Name            string
	DisplayName     string
	SkillsDir       string // relative, project-level skills directory path
	GlobalSkillsDir string // absolute, user-level path (empty = global not supported)
	// ExcludeFromPicker, when true, hides this harness from the locked section of
	// the harness picker in `mdm skills add`. Only a small handful of shared-dir
	// harnesses (replit, universal) are excluded; all others are shown by default.
	ExcludeFromPicker bool

	// InstructionsFile is the project-root path to this harness's instruction
	// file (e.g. "CLAUDE.md", ".cursorrules"). Empty means no instruction file.
	InstructionsFile string

	// SharedSkillsDir is true when this harness reads skills from the shared
	// .agents/skills directory. Skills installed there are available
	// automatically - no per-harness skills directory needs to be configured.
	SharedSkillsDir bool

	// NativeInstructions is true when this harness reads AGENTS.md natively or
	// has no per-project instruction file. When true, no symlink to AGENTS.md
	// is needed and configuredHarnesses does not need to track this harness for rules.
	NativeInstructions bool

	// AgentsInstallDir is the project-relative directory this harness reads
	// agent definitions from. Empty means mdm has no directory recorded for
	// this harness, and an install skips it with a notice - not a claim that
	// the harness lacks the concept. GlobalAgentsInstallDir is the user-level one.
	AgentsInstallDir       string
	GlobalAgentsInstallDir string

	// AgentFileSuffix overrides the default ".md" for files this harness
	// reads from AgentsInstallDir. GitHub Copilot CLI is the known
	// exception: it loads only files ending in ".agent.md".
	AgentFileSuffix string

	// AgentFileFormat is the on-disk shape of a definition this harness
	// reads: markdown frontmatter or TOML. AgentFileSuffix says what the
	// file is called; this says what it is. Empty defaults to markdown via
	// AgentFormat, so most entries leave it unset.
	AgentFileFormat agentfile.Format

	// AgentAlwaysMaterialize is true when a symlinked agent definition is
	// unsafe for this harness's AgentsInstallDir and a real file copy must
	// be written instead. See the github-copilot entry for why.
	AgentAlwaysMaterialize bool

	// AgentNamePattern documents this harness's naming constraint on agent
	// definition names, as prose or a regex fragment. Empty means mdm has
	// not recorded a constraint for this harness.
	AgentNamePattern string

	DetectInstalled func() bool
}

const SharedRootDir = ".agents"
const SkillsSubdir = "skills"
const AgentsSubdir = "agents"

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

var AllHarnesses map[string]*HarnessConfig

func init() {
	Reload()
}

// Reload rebuilds AllHarnesses from the current environment. Global install
// paths resolve once at init from the home directory and the XDG and
// harness-specific variables, so a test that redirects those must call this.
// AllHarnesses is replaced wholesale, so such tests must not run in parallel.
func Reload() {
	home, _ := os.UserHomeDir()
	configHome := getXDGConfigHome()
	codexHome := getCodexHome()
	claudeHome := getClaudeHome()

	// ── SharedSkillsDir=true + NativeInstructions=false ────────────────────────
	// Harnesses that use .agents/skills but have their own instruction file.
	// Skills are auto-covered; only the rules symlink needs to be configured.

	sharedSkillsUniqueRules := map[string]*HarnessConfig{
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
			// https://cursor.com/docs/subagents - checked 2026-09-03. Project
			// and user dirs both hold plain .md files with name/description
			// frontmatter.
			AgentsInstallDir:       ".cursor/agents",
			GlobalAgentsInstallDir: filepath.Join(home, ".cursor/agents"),
			DetectInstalled:        func() bool { return pathExists(filepath.Join(home, ".cursor")) },
		},
		"gemini-cli": {
			Name:               "gemini-cli",
			DisplayName:        "Gemini CLI",
			SkillsDir:          ".agents/skills",
			GlobalSkillsDir:    filepath.Join(home, ".gemini/skills"),
			InstructionsFile:   "GEMINI.md",
			SharedSkillsDir:    true,
			NativeInstructions: false,
			// https://geminicli.com/docs/core/subagents/ - checked 2026-09-03.
			// Project and user dirs both hold plain .md files.
			AgentsInstallDir:       ".gemini/agents",
			GlobalAgentsInstallDir: filepath.Join(home, ".gemini/agents"),
			// https://github.com/google-gemini/gemini-cli/blob/main/docs/core/subagents.md
			// - checked 2026-09-05. Lowercase letters, digits, hyphens,
			// underscores.
			AgentNamePattern: "lowercase letters, digits, hyphens, underscores",
			DetectInstalled:  func() bool { return pathExists(filepath.Join(home, ".gemini")) },
		},
		"github-copilot": {
			Name:               "github-copilot",
			DisplayName:        "GitHub Copilot",
			SkillsDir:          ".agents/skills",
			GlobalSkillsDir:    filepath.Join(home, ".copilot/skills"),
			InstructionsFile:   ".github/copilot-instructions.md",
			SharedSkillsDir:    true,
			NativeInstructions: false,
			// https://docs.github.com/en/copilot/how-tos/copilot-cli/customize-copilot/create-custom-agents-for-cli
			// - checked 2026-09-03. Copilot CLI loads only files ending
			// ".agent.md". That page confirms the precedence: a user file in
			// ~/.copilot/agents beats the project's .github/agents, the
			// opposite of claude-code. Deliberate; do not align it with those.
			AgentsInstallDir:       ".github/agents",
			GlobalAgentsInstallDir: filepath.Join(home, ".copilot/agents"),
			AgentFileSuffix:        ".agent.md",
			// .github is committed and holds workflows and CODEOWNERS, so it
			// is never ignored wholesale. GitHub documents repo-scoped agents
			// as the way to share them through the repository. A symlink
			// committed there arrives on a teammate's Windows checkout as a
			// text file holding a path, not the agent definition.
			AgentAlwaysMaterialize: true,
			DetectInstalled:        func() bool { return pathExists(filepath.Join(home, ".copilot")) },
		},
	}

	// ── SharedSkillsDir=true + NativeInstructions=true ────────────────────────
	// Fully automatic: skills come from .agents/skills and instructions from
	// AGENTS.md. Nothing to configure.

	fullyAutomatic := map[string]*HarnessConfig{
		"antigravity": {
			Name:               "antigravity",
			DisplayName:        "Antigravity",
			SkillsDir:          ".agents/skills",
			GlobalSkillsDir:    filepath.Join(home, ".gemini/antigravity/skills"),
			SharedSkillsDir:    true,
			NativeInstructions: true,
			DetectInstalled:    func() bool { return pathExists(filepath.Join(home, ".gemini/antigravity")) },
		},
		// Codex has custom agents: standalone .toml files (name, description,
		// developer_instructions) in .codex/agents and ~/.codex/agents.
		// https://learn.chatgpt.com/docs/agent-configuration/subagents -
		// checked 2026-09-05.
		"codex": {
			Name:                   "codex",
			DisplayName:            "Codex",
			SkillsDir:              ".agents/skills",
			GlobalSkillsDir:        filepath.Join(codexHome, "skills"),
			InstructionsFile:       "AGENTS.md",
			SharedSkillsDir:        true,
			NativeInstructions:     true,
			AgentsInstallDir:       ".codex/agents",
			GlobalAgentsInstallDir: filepath.Join(codexHome, "agents"),
			AgentFileSuffix:        ".toml",
			AgentFileFormat:        agentfile.FormatTOML,
			DetectInstalled:        func() bool { return pathExists(codexHome) || pathExists("/etc/codex") },
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
			// https://opencode.ai/docs/agents/ - checked 2026-09-03. The
			// loader uses the plural "agents" in both directories.
			AgentsInstallDir:       ".opencode/agents",
			GlobalAgentsInstallDir: filepath.Join(configHome, "opencode/agents"),
			DetectInstalled:        func() bool { return pathExists(filepath.Join(configHome, "opencode")) },
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
	// Harnesses that need both a dedicated skills directory AND a rules symlink.
	// Both must be explicitly configured via configuredHarnesses.

	uniqueSkillsUniqueRules := map[string]*HarnessConfig{
		"claude-code": {
			Name:               "claude-code",
			DisplayName:        "Claude Code",
			SkillsDir:          ".claude/skills",
			GlobalSkillsDir:    filepath.Join(claudeHome, "skills"),
			InstructionsFile:   "CLAUDE.md",
			SharedSkillsDir:    false,
			NativeInstructions: false,
			// https://code.claude.com/docs/en/sub-agents - checked 2026-09-03.
			// Project and user dirs both hold plain .md files with name and
			// description frontmatter, scanned recursively.
			AgentsInstallDir:       ".claude/agents",
			GlobalAgentsInstallDir: filepath.Join(claudeHome, "agents"),
			// https://code.claude.com/docs/en/sub-agents - checked
			// 2026-09-05. Lowercase letters and hyphens; must not contain ":".
			AgentNamePattern: "lowercase letters and hyphens, no colon",
			DetectInstalled:  func() bool { return pathExists(claudeHome) },
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

	// ── SharedSkillsDir=false + NativeInstructions=true ───────────────────────
	// Harnesses with their own skills directory but no per-project instruction
	// file. Only the skills directory needs configuring.

	uniqueSkillsNativeRules := map[string]*HarnessConfig{
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

	AllHarnesses = make(map[string]*HarnessConfig)
	for k, v := range sharedSkillsUniqueRules {
		AllHarnesses[k] = v
	}
	for k, v := range fullyAutomatic {
		AllHarnesses[k] = v
	}
	for k, v := range uniqueSkillsUniqueRules {
		AllHarnesses[k] = v
	}
	for k, v := range uniqueSkillsNativeRules {
		AllHarnesses[k] = v
	}
}

func DetectInstalledHarnesses() []string {
	var installed []string
	for name, a := range AllHarnesses {
		if a.DetectInstalled() {
			installed = append(installed, name)
		}
	}
	return installed
}

// ─── Harness classification helpers ──────────────────────────────────
//
// These read the SharedSkillsDir and NativeInstructions fields. Do not add
// string comparisons against SkillsDir or InstructionsFile in calling code.

// UsesSharedSkillsDir reports whether the harness reads skills from the shared
// .agents/skills directory (SharedSkillsDir == true).
func UsesSharedSkillsDir(name string) bool {
	a, ok := AllHarnesses[name]
	return ok && a.SharedSkillsDir
}

// canonicalSharedDir resolves the shared .agents/<subdir> directory for a
// scope: under the user's home in global scope, under cwd in project scope. An
// empty cwd means the current working directory.
func canonicalSharedDir(subdir string, global bool, cwd string) string {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	baseDir := cwd
	if global {
		baseDir, _ = os.UserHomeDir()
	}
	return filepath.Join(baseDir, SharedRootDir, subdir)
}

// CanonicalSkillsDir returns the shared .agents/skills directory for a scope:
// under the user's home in global scope, under cwd in project scope. An empty
// cwd means the current working directory.
func CanonicalSkillsDir(global bool, cwd string) string {
	return canonicalSharedDir(SkillsSubdir, global, cwd)
}

// CanonicalAgentsDir returns the shared .agents/agents directory for a scope,
// by the same rule as CanonicalSkillsDir. It is only the location of mdm's own
// canonical copy; AgentsInstallDirFor never consults it.
func CanonicalAgentsDir(global bool, cwd string) string {
	return canonicalSharedDir(AgentsSubdir, global, cwd)
}

// SkillsInstallDir returns the directory a harness reads its skills from in the
// given scope, or "" when the harness is unknown or has no directory there.
// Callers treating the shared .agents/skills directory specially must still
// check UsesSharedSkillsDir. TODO: harnessDirForScope, isSkillInstalled,
// checkHarnessLinks, and cleanUpRemovedHarnessFiles still build it themselves.
func SkillsInstallDir(name string, global bool, cwd string) string {
	a := AllHarnesses[name]
	if a == nil {
		return ""
	}
	if a.SharedSkillsDir {
		return CanonicalSkillsDir(global, cwd)
	}
	if global {
		if a.GlobalSkillsDir != "" {
			return a.GlobalSkillsDir
		}
		if a.SkillsDir == "" {
			return ""
		}
		home, _ := os.UserHomeDir()
		return filepath.Join(home, a.SkillsDir)
	}
	if a.SkillsDir == "" {
		return ""
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	return filepath.Join(cwd, a.SkillsDir)
}

// AgentFileExt returns the extension harnessName expects for a file under
// its AgentsInstallDir, defaulting to ".md" for a harness with no override
// and for an unknown name.
func AgentFileExt(harnessName string) string {
	if h, ok := AllHarnesses[harnessName]; ok && h.AgentFileSuffix != "" {
		return h.AgentFileSuffix
	}
	return ".md"
}

// AgentFormat returns the on-disk shape harnessName reads agent definitions
// in, defaulting to markdown for a harness with no override and for an
// unknown name.
func AgentFormat(harnessName string) agentfile.Format {
	if h, ok := AllHarnesses[harnessName]; ok && h.AgentFileFormat != "" {
		return h.AgentFileFormat
	}
	return agentfile.FormatMarkdown
}

// AgentsInstallDirFor resolves where harnessName reads agent definitions
// for a scope. It returns "" when mdm has no directory recorded for the
// harness, or when global scope is asked of a harness with no user-level
// directory.
func AgentsInstallDirFor(name string, global bool, cwd string) string {
	h, ok := AllHarnesses[name]
	if !ok {
		return ""
	}
	if global {
		return h.GlobalAgentsInstallDir
	}
	if h.AgentsInstallDir == "" {
		return ""
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	return filepath.Join(cwd, h.AgentsInstallDir)
}

// NeedsNoTracking reports whether a harness requires no entry in configuredHarnesses.
// True when both skills and instructions are auto-covered
// (SharedSkillsDir && NativeInstructions).
func NeedsNoTracking(name string) bool {
	a, ok := AllHarnesses[name]
	return ok && a.SharedSkillsDir && a.NativeInstructions
}

// GetSharedSkillsDirHarnesses returns harnesses that use .agents/skills and are
// shown in the locked section of the harness picker (ExcludeFromPicker == false).
func GetSharedSkillsDirHarnesses() []string {
	var result []string
	for name, a := range AllHarnesses {
		if a.SharedSkillsDir && !a.ExcludeFromPicker {
			result = append(result, name)
		}
	}
	return result
}

// GetUniqueSkillsDirHarnesses returns harnesses with their own dedicated skills
// directory. These always need explicit configuration.
func GetUniqueSkillsDirHarnesses() []string {
	var result []string
	for name, a := range AllHarnesses {
		if !a.SharedSkillsDir {
			result = append(result, name)
		}
	}
	return result
}
