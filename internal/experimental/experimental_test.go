package experimental

import "testing"

// isolate points the global lock at a fresh temp dir so tests never read or
// write the developer's real lock file.
func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv(EnvVar, "")
}

func TestEnabledByEnv(t *testing.T) {
	isolate(t)
	cases := []struct {
		env  string
		want bool
	}{
		{"", false},
		{"knowledge", true},
		{"KNOWLEDGE", true},
		{"  knowledge  ", true},
		{"other,knowledge", true},
		{"other, knowledge", true},
		{"all", true},
		{"ALL", true},
		{"other", false},
		{"knowledgeable", false},
	}
	for _, c := range cases {
		t.Setenv(EnvVar, c.env)
		if got := Enabled(Knowledge); got != c.want {
			t.Errorf("MDM_EXPERIMENTAL=%q: Enabled(Knowledge)=%v, want %v", c.env, got, c.want)
		}
	}
}

func TestEnableDisablePersists(t *testing.T) {
	isolate(t)

	if Enabled(Knowledge) {
		t.Fatal("expected knowledge to be disabled by default")
	}
	if err := Enable(Knowledge); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if !Persisted(Knowledge) {
		t.Fatal("expected knowledge to be persisted after Enable")
	}
	if !Enabled(Knowledge) {
		t.Fatal("expected knowledge to be enabled after Enable")
	}

	// Enable is idempotent.
	if err := Enable(Knowledge); err != nil {
		t.Fatalf("second Enable: %v", err)
	}

	if err := Disable(Knowledge); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if Enabled(Knowledge) {
		t.Fatal("expected knowledge to be disabled after Disable")
	}

	// Disable on an already-disabled feature is a no-op.
	if err := Disable(Knowledge); err != nil {
		t.Fatalf("second Disable: %v", err)
	}
}

func TestEnvWinsOverDisable(t *testing.T) {
	isolate(t)
	t.Setenv(EnvVar, "knowledge")
	if err := Disable(Knowledge); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if !Enabled(Knowledge) {
		t.Fatal("expected MDM_EXPERIMENTAL to win over persisted disable")
	}
}

func TestIsKnown(t *testing.T) {
	if !IsKnown("knowledge") {
		t.Error("expected knowledge to be a known feature")
	}
	if IsKnown("nonsense") {
		t.Error("expected nonsense to be unknown")
	}
}
