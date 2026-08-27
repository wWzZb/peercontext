package v2state

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestV2StateIsIndependentAndStoresKeysOutsideStateJSON(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PEERCTX_TEST_KEY_DIR", filepath.Join(root, "keys"))
	manager, err := NewManager(filepath.Join(root, "v2"))
	if err != nil {
		t.Fatal(err)
	}
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	profile := Profile{ProjectID: "prj", ProjectName: "test", MemberID: "mem", MemberName: "alice", HostPublicKey: public, Hosted: true}
	if err = manager.PutProfile(profile, private); err != nil {
		t.Fatal(err)
	}
	loaded, err := manager.PrivateKey("prj")
	if err != nil || !private.Equal(loaded) {
		t.Fatalf("PrivateKey: %v", err)
	}
	state, _, err := manager.Current()
	if err != nil || state.SchemaVersion != 2 {
		t.Fatalf("Current: %#v %v", state, err)
	}
	stateJSON, err := os.ReadFile(filepath.Join(root, "v2", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stateJSON, []byte(base64.RawURLEncoding.EncodeToString(private))) {
		t.Fatal("Project private key was written into state.json")
	}
}
