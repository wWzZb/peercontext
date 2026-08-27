package v2state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyV1StateIsIgnoredWithExplicitMessage(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "state.json"), []byte(`{"schema_version":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	manager, _ := NewManager(filepath.Join(root, "v2"))
	state, err := manager.Load()
	if err != nil || state.SchemaVersion != 2 || len(state.Profiles) != 0 {
		t.Fatalf("v2 state = %#v, err = %v", state, err)
	}
	_, _, err = manager.Current()
	if err == nil || !strings.Contains(err.Error(), "intentionally incompatible") {
		t.Fatalf("Current error = %v", err)
	}
}
