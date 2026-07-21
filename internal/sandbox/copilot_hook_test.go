package sandbox

import (
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

// TestCopilotHookScript feeds real preToolUse payloads through the generated
// guard script under sh, verifying the deny decisions end to end (including
// grep/sed compatibility).
func TestCopilotHookScript(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("guard script is POSIX sh")
	}
	isolate(t)
	project := t.TempDir()
	if _, err := copilotSetup(project, true); err != nil {
		t.Fatal(err)
	}
	script := copilotHookScriptPath(project)

	cases := []struct {
		name    string
		payload string
		deny    bool
	}{
		{"read dotenv", `{"toolName":"view","toolArgs":{"path":"/repo/.env"}}`, true},
		{"read dotenv variant", `{"toolName":"view","toolArgs":{"path":".env.local"}}`, true},
		{"cat aws credentials", `{"toolName":"bash","toolArgs":{"command":"cat ~/.aws/credentials"}}`, true},
		{"read ssh key", `{"toolName":"bash","toolArgs":{"command":"cat ~/.ssh/id_rsa"}}`, true},
		{"write pem", `{"toolName":"create","toolArgs":{"path":"certs/server.pem","content":"x"}}`, true},
		{"secrets dir", `{"toolName":"grep","toolArgs":{"path":"secrets/prod.yaml","pattern":"pass"}}`, true},
		{"normal file", `{"toolName":"view","toolArgs":{"path":"main.go"}}`, false},
		{"process.env idiom", `{"toolName":"bash","toolArgs":{"command":"node -e \"console.log(process.env.CI)\""}}`, false},
		{"env template", `{"toolName":"view","toolArgs":{"path":".env.example"}}`, false},
		{"monkey business", `{"toolName":"bash","toolArgs":{"command":"grep monkey donkey.keys.go"}}`, false},
		{"unguarded tool", `{"toolName":"web_search","toolArgs":{"query":"what is a .env file"}}`, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command("sh", script)
			cmd.Stdin = strings.NewReader(tc.payload)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("script failed: %v\n%s", err, out)
			}
			denied := strings.Contains(string(out), `"permissionDecision":"deny"`)
			if denied != tc.deny {
				t.Errorf("payload %s: denied=%v, want %v (output: %s)", tc.payload, denied, tc.deny, out)
			}
		})
	}
}
