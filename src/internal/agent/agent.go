package agent

import (
	"os"
	"path/filepath"
	"strings"
)

type AgentConfig struct {
	Name             string
	DisplayName      string
	SkillsDir        string // relative, project-level
	GlobalSkillsDir  string // absolute, user-level (empty = not supported)
	ShowInUniversal  bool   // default true; false to hide from universal list
	InstructionsFile string // project-root path to this agent's instructions file (empty = unknown)
	DetectInstalled  func() bool
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

	AllAgents = map[string]*AgentConfig{
		"amp": {
			Name:             "amp",
			DisplayName:      "Amp",
			SkillsDir:        ".agents/skills",
			GlobalSkillsDir:  filepath.Join(configHome, "agents/skills"),
			ShowInUniversal:  true,
			InstructionsFile: "AMP.md",
			DetectInstalled:  func() bool { return pathExists(filepath.Join(configHome, "amp")) },
		},
		"antigravity": {
			Name:            "antigravity",
			DisplayName:     "Antigravity",
			SkillsDir:       ".agents/skills",
			GlobalSkillsDir: filepath.Join(home, ".gemini/antigravity/skills"),
			ShowInUniversal: true,
			DetectInstalled: func() bool { return pathExists(filepath.Join(home, ".gemini/antigravity")) },
		},
		"augment": {
			Name:            "augment",
			DisplayName:     "Augment",
			SkillsDir:       ".augment/skills",
			GlobalSkillsDir: filepath.Join(home, ".augment/skills"),
			ShowInUniversal: true,
			DetectInstalled: func() bool { return pathExists(filepath.Join(home, ".augment")) },
		},
		"bob": {
			Name:            "bob",
			DisplayName:     "IBM Bob",
			SkillsDir:       ".bob/skills",
			GlobalSkillsDir: filepath.Join(home, ".bob/skills"),
			ShowInUniversal: true,
			DetectInstalled: func() bool { return pathExists(filepath.Join(home, ".bob")) },
		},
		"claude-code": {
			Name:             "claude-code",
			DisplayName:      "Claude Code",
			SkillsDir:        ".claude/skills",
			GlobalSkillsDir:  filepath.Join(claudeHome, "skills"),
			ShowInUniversal:  true,
			InstructionsFile: "CLAUDE.md",
			DetectInstalled:  func() bool { return pathExists(claudeHome) },
		},
		"openclaw": {
			Name:            "openclaw",
			DisplayName:     "OpenClaw",
			SkillsDir:       "skills",
			GlobalSkillsDir: getOpenClawGlobalSkillsDir(),
			ShowInUniversal: true,
			DetectInstalled: func() bool {
				return pathExists(filepath.Join(home, ".openclaw")) ||
					pathExists(filepath.Join(home, ".clawdbot")) ||
					pathExists(filepath.Join(home, ".moltbot"))
			},
		},
		"cline": {
			Name:             "cline",
			DisplayName:      "Cline",
			SkillsDir:        ".agents/skills",
			GlobalSkillsDir:  filepath.Join(home, ".agents/skills"),
			ShowInUniversal:  true,
			InstructionsFile: ".clinerules",
			DetectInstalled:  func() bool { return pathExists(filepath.Join(home, ".cline")) },
		},
		"codebuddy": {
			Name:            "codebuddy",
			DisplayName:     "CodeBuddy",
			SkillsDir:       ".codebuddy/skills",
			GlobalSkillsDir: filepath.Join(home, ".codebuddy/skills"),
			ShowInUniversal: true,
			DetectInstalled: func() bool {
				cwd, _ := os.Getwd()
				return pathExists(filepath.Join(cwd, ".codebuddy")) || pathExists(filepath.Join(home, ".codebuddy"))
			},
		},
		"codex": {
			Name:             "codex",
			DisplayName:      "Codex",
			SkillsDir:        ".agents/skills",
			GlobalSkillsDir:  filepath.Join(codexHome, "skills"),
			ShowInUniversal:  true,
			InstructionsFile: "AGENTS.md",
			DetectInstalled:  func() bool { return pathExists(codexHome) || pathExists("/etc/codex") },
		},
		"command-code": {
			Name:            "command-code",
			DisplayName:     "Command Code",
			SkillsDir:       ".commandcode/skills",
			GlobalSkillsDir: filepath.Join(home, ".commandcode/skills"),
			ShowInUniversal: true,
			DetectInstalled: func() bool { return pathExists(filepath.Join(home, ".commandcode")) },
		},
		"continue": {
			Name:            "continue",
			DisplayName:     "Continue",
			SkillsDir:       ".continue/skills",
			GlobalSkillsDir: filepath.Join(home, ".continue/skills"),
			ShowInUniversal: true,
			DetectInstalled: func() bool {
				cwd, _ := os.Getwd()
				return pathExists(filepath.Join(cwd, ".continue")) || pathExists(filepath.Join(home, ".continue"))
			},
		},
		"cortex": {
			Name:            "cortex",
			DisplayName:     "Cortex Code",
			SkillsDir:       ".cortex/skills",
			GlobalSkillsDir: filepath.Join(home, ".snowflake/cortex/skills"),
			ShowInUniversal: true,
			DetectInstalled: func() bool { return pathExists(filepath.Join(home, ".snowflake/cortex")) },
		},
		"crush": {
			Name:            "crush",
			DisplayName:     "Crush",
			SkillsDir:       ".crush/skills",
			GlobalSkillsDir: filepath.Join(home, ".config/crush/skills"),
			ShowInUniversal: true,
			DetectInstalled: func() bool { return pathExists(filepath.Join(home, ".config/crush")) },
		},
		"cursor": {
			Name:             "cursor",
			DisplayName:      "Cursor",
			SkillsDir:        ".agents/skills",
			GlobalSkillsDir:  filepath.Join(home, ".cursor/skills"),
			ShowInUniversal:  true,
			InstructionsFile: ".cursorrules",
			DetectInstalled:  func() bool { return pathExists(filepath.Join(home, ".cursor")) },
		},
		"deepagents": {
			Name:            "deepagents",
			DisplayName:     "Deep Agents",
			SkillsDir:       ".agents/skills",
			GlobalSkillsDir: filepath.Join(home, ".deepagents/agent/skills"),
			ShowInUniversal: true,
			DetectInstalled: func() bool { return pathExists(filepath.Join(home, ".deepagents")) },
		},
		"droid": {
			Name:            "droid",
			DisplayName:     "Droid",
			SkillsDir:       ".factory/skills",
			GlobalSkillsDir: filepath.Join(home, ".factory/skills"),
			ShowInUniversal: true,
			DetectInstalled: func() bool { return pathExists(filepath.Join(home, ".factory")) },
		},
		"firebender": {
			Name:            "firebender",
			DisplayName:     "Firebender",
			SkillsDir:       ".agents/skills",
			GlobalSkillsDir: filepath.Join(home, ".firebender/skills"),
			ShowInUniversal: true,
			DetectInstalled: func() bool { return pathExists(filepath.Join(home, ".firebender")) },
		},
		"gemini-cli": {
			Name:             "gemini-cli",
			DisplayName:      "Gemini CLI",
			SkillsDir:        ".agents/skills",
			GlobalSkillsDir:  filepath.Join(home, ".gemini/skills"),
			ShowInUniversal:  true,
			InstructionsFile: "GEMINI.md",
			DetectInstalled:  func() bool { return pathExists(filepath.Join(home, ".gemini")) },
		},
		"github-copilot": {
			Name:             "github-copilot",
			DisplayName:      "GitHub Copilot",
			SkillsDir:        ".agents/skills",
			GlobalSkillsDir:  filepath.Join(home, ".copilot/skills"),
			ShowInUniversal:  true,
			InstructionsFile: ".github/copilot-instructions.md",
			DetectInstalled:  func() bool { return pathExists(filepath.Join(home, ".copilot")) },
		},
		"goose": {
			Name:            "goose",
			DisplayName:     "Goose",
			SkillsDir:       ".goose/skills",
			GlobalSkillsDir: filepath.Join(configHome, "goose/skills"),
			ShowInUniversal: true,
			DetectInstalled: func() bool { return pathExists(filepath.Join(configHome, "goose")) },
		},
		"iflow-cli": {
			Name:            "iflow-cli",
			DisplayName:     "iFlow CLI",
			SkillsDir:       ".iflow/skills",
			GlobalSkillsDir: filepath.Join(home, ".iflow/skills"),
			ShowInUniversal: true,
			DetectInstalled: func() bool { return pathExists(filepath.Join(home, ".iflow")) },
		},
		"junie": {
			Name:            "junie",
			DisplayName:     "Junie",
			SkillsDir:       ".junie/skills",
			GlobalSkillsDir: filepath.Join(home, ".junie/skills"),
			ShowInUniversal: true,
			DetectInstalled: func() bool { return pathExists(filepath.Join(home, ".junie")) },
		},
		"kilo": {
			Name:            "kilo",
			DisplayName:     "Kilo Code",
			SkillsDir:       ".kilocode/skills",
			GlobalSkillsDir: filepath.Join(home, ".kilocode/skills"),
			ShowInUniversal: true,
			DetectInstalled: func() bool { return pathExists(filepath.Join(home, ".kilocode")) },
		},
		"kimi-cli": {
			Name:            "kimi-cli",
			DisplayName:     "Kimi Code CLI",
			SkillsDir:       ".agents/skills",
			GlobalSkillsDir: filepath.Join(home, ".config/agents/skills"),
			ShowInUniversal: true,
			DetectInstalled: func() bool { return pathExists(filepath.Join(home, ".kimi")) },
		},
		"kiro-cli": {
			Name:            "kiro-cli",
			DisplayName:     "Kiro CLI",
			SkillsDir:       ".kiro/skills",
			GlobalSkillsDir: filepath.Join(home, ".kiro/skills"),
			ShowInUniversal: true,
			DetectInstalled: func() bool { return pathExists(filepath.Join(home, ".kiro")) },
		},
		"kode": {
			Name:            "kode",
			DisplayName:     "Kode",
			SkillsDir:       ".kode/skills",
			GlobalSkillsDir: filepath.Join(home, ".kode/skills"),
			ShowInUniversal: true,
			DetectInstalled: func() bool { return pathExists(filepath.Join(home, ".kode")) },
		},
		"mcpjam": {
			Name:            "mcpjam",
			DisplayName:     "MCPJam",
			SkillsDir:       ".mcpjam/skills",
			GlobalSkillsDir: filepath.Join(home, ".mcpjam/skills"),
			ShowInUniversal: true,
			DetectInstalled: func() bool { return pathExists(filepath.Join(home, ".mcpjam")) },
		},
		"mistral-vibe": {
			Name:            "mistral-vibe",
			DisplayName:     "Mistral Vibe",
			SkillsDir:       ".vibe/skills",
			GlobalSkillsDir: filepath.Join(home, ".vibe/skills"),
			ShowInUniversal: true,
			DetectInstalled: func() bool { return pathExists(filepath.Join(home, ".vibe")) },
		},
		"mux": {
			Name:            "mux",
			DisplayName:     "Mux",
			SkillsDir:       ".mux/skills",
			GlobalSkillsDir: filepath.Join(home, ".mux/skills"),
			ShowInUniversal: true,
			DetectInstalled: func() bool { return pathExists(filepath.Join(home, ".mux")) },
		},
		"neovate": {
			Name:            "neovate",
			DisplayName:     "Neovate",
			SkillsDir:       ".neovate/skills",
			GlobalSkillsDir: filepath.Join(home, ".neovate/skills"),
			ShowInUniversal: true,
			DetectInstalled: func() bool { return pathExists(filepath.Join(home, ".neovate")) },
		},
		"opencode": {
			Name:             "opencode",
			DisplayName:      "OpenCode",
			SkillsDir:        ".agents/skills",
			GlobalSkillsDir:  filepath.Join(configHome, "opencode/skills"),
			ShowInUniversal:  true,
			InstructionsFile: "AGENTS.md",
			DetectInstalled:  func() bool { return pathExists(filepath.Join(configHome, "opencode")) },
		},
		"openhands": {
			Name:            "openhands",
			DisplayName:     "OpenHands",
			SkillsDir:       ".openhands/skills",
			GlobalSkillsDir: filepath.Join(home, ".openhands/skills"),
			ShowInUniversal: true,
			DetectInstalled: func() bool { return pathExists(filepath.Join(home, ".openhands")) },
		},
		"pi": {
			Name:            "pi",
			DisplayName:     "Pi",
			SkillsDir:       ".pi/skills",
			GlobalSkillsDir: filepath.Join(home, ".pi/agent/skills"),
			ShowInUniversal: true,
			DetectInstalled: func() bool { return pathExists(filepath.Join(home, ".pi/agent")) },
		},
		"pochi": {
			Name:            "pochi",
			DisplayName:     "Pochi",
			SkillsDir:       ".pochi/skills",
			GlobalSkillsDir: filepath.Join(home, ".pochi/skills"),
			ShowInUniversal: true,
			DetectInstalled: func() bool { return pathExists(filepath.Join(home, ".pochi")) },
		},
		"adal": {
			Name:            "adal",
			DisplayName:     "AdaL",
			SkillsDir:       ".adal/skills",
			GlobalSkillsDir: filepath.Join(home, ".adal/skills"),
			ShowInUniversal: true,
			DetectInstalled: func() bool { return pathExists(filepath.Join(home, ".adal")) },
		},
		"qoder": {
			Name:            "qoder",
			DisplayName:     "Qoder",
			SkillsDir:       ".qoder/skills",
			GlobalSkillsDir: filepath.Join(home, ".qoder/skills"),
			ShowInUniversal: true,
			DetectInstalled: func() bool { return pathExists(filepath.Join(home, ".qoder")) },
		},
		"qwen-code": {
			Name:            "qwen-code",
			DisplayName:     "Qwen Code",
			SkillsDir:       ".qwen/skills",
			GlobalSkillsDir: filepath.Join(home, ".qwen/skills"),
			ShowInUniversal: true,
			DetectInstalled: func() bool { return pathExists(filepath.Join(home, ".qwen")) },
		},
		"replit": {
			Name:             "replit",
			DisplayName:      "Replit",
			SkillsDir:        ".agents/skills",
			GlobalSkillsDir:  filepath.Join(configHome, "agents/skills"),
			ShowInUniversal:  false,
			InstructionsFile: "AGENTS.md",
			DetectInstalled: func() bool {
				cwd, _ := os.Getwd()
				return pathExists(filepath.Join(cwd, ".replit"))
			},
		},
		"roo": {
			Name:             "roo",
			DisplayName:      "Roo Code",
			SkillsDir:        ".roo/skills",
			GlobalSkillsDir:  filepath.Join(home, ".roo/skills"),
			ShowInUniversal:  true,
			InstructionsFile: ".roorules",
			DetectInstalled:  func() bool { return pathExists(filepath.Join(home, ".roo")) },
		},
		"trae": {
			Name:            "trae",
			DisplayName:     "Trae",
			SkillsDir:       ".trae/skills",
			GlobalSkillsDir: filepath.Join(home, ".trae/skills"),
			ShowInUniversal: true,
			DetectInstalled: func() bool { return pathExists(filepath.Join(home, ".trae")) },
		},
		"trae-cn": {
			Name:            "trae-cn",
			DisplayName:     "Trae CN",
			SkillsDir:       ".trae/skills",
			GlobalSkillsDir: filepath.Join(home, ".trae-cn/skills"),
			ShowInUniversal: true,
			DetectInstalled: func() bool { return pathExists(filepath.Join(home, ".trae-cn")) },
		},
		"warp": {
			Name:            "warp",
			DisplayName:     "Warp",
			SkillsDir:       ".agents/skills",
			GlobalSkillsDir: filepath.Join(home, ".agents/skills"),
			ShowInUniversal: true,
			DetectInstalled: func() bool { return pathExists(filepath.Join(home, ".warp")) },
		},
		"windsurf": {
			Name:             "windsurf",
			DisplayName:      "Windsurf",
			SkillsDir:        ".windsurf/skills",
			GlobalSkillsDir:  filepath.Join(home, ".codeium/windsurf/skills"),
			ShowInUniversal:  true,
			InstructionsFile: ".windsurfrules",
			DetectInstalled:  func() bool { return pathExists(filepath.Join(home, ".codeium/windsurf")) },
		},
		"zencoder": {
			Name:            "zencoder",
			DisplayName:     "Zencoder",
			SkillsDir:       ".zencoder/skills",
			GlobalSkillsDir: filepath.Join(home, ".zencoder/skills"),
			ShowInUniversal: true,
			DetectInstalled: func() bool { return pathExists(filepath.Join(home, ".zencoder")) },
		},
		"universal": {
			Name:            "universal",
			DisplayName:     "Universal",
			SkillsDir:       ".agents/skills",
			GlobalSkillsDir: filepath.Join(configHome, "agents/skills"),
			ShowInUniversal: false,
			DetectInstalled: func() bool { return false },
		},
	}
}

func GetAgentNames() []string {
	names := make([]string, 0, len(AllAgents))
	for k := range AllAgents {
		names = append(names, k)
	}
	return names
}

func IsValidAgent(name string) bool {
	_, ok := AllAgents[name]
	return ok
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

func IsUniversalAgent(name string) bool {
	a, ok := AllAgents[name]
	if !ok {
		return false
	}
	return a.SkillsDir == ".agents/skills"
}

func GetUniversalAgents() []string {
	var result []string
	for name, a := range AllAgents {
		if a.SkillsDir == ".agents/skills" && a.ShowInUniversal {
			result = append(result, name)
		}
	}
	return result
}

func GetNonUniversalAgents() []string {
	var result []string
	for name, a := range AllAgents {
		if a.SkillsDir != ".agents/skills" {
			result = append(result, name)
		}
	}
	return result
}

func EnsureUniversalAgents(targetAgents []string) []string {
	universal := GetUniversalAgents()
	result := make([]string, len(targetAgents))
	copy(result, targetAgents)
	for _, ua := range universal {
		found := false
		for _, t := range result {
			if t == ua {
				found = true
				break
			}
		}
		if !found {
			result = append(result, ua)
		}
	}
	return result
}
