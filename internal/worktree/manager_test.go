package worktree

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestDetachedWorktreeLifecyclePreservesMainCheckoutAndGitState(t *testing.T) {
	repository := testRepository(t)
	manager, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	beforeHead := gitOutput(t, repository, "rev-parse", "HEAD")
	beforeBranch := gitOutput(t, repository, "symbolic-ref", "HEAD")
	beforeRemote := gitOutput(t, repository, "remote", "get-url", "origin")
	mainBytes, _ := os.ReadFile(filepath.Join(repository, "fixture.txt"))

	record, err := manager.Create(repository, "agt_writer", "req_write", beforeHead)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if gitOutput(t, record.Path, "rev-parse", "--abbrev-ref", "HEAD") != "HEAD" {
		t.Fatal("created worktree is not detached")
	}
	if gitOutput(t, record.Path, "rev-parse", "HEAD") != beforeHead || record.BaseCommit != beforeHead {
		t.Fatalf("worktree base = %s / %s", record.BaseCommit, gitOutput(t, record.Path, "rev-parse", "HEAD"))
	}
	if err := os.WriteFile(filepath.Join(record.Path, "fixture.txt"), []byte("worktree change\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(repository, "fixture.txt")); string(got) != string(mainBytes) {
		t.Fatal("detached worktree modified the main checkout")
	}
	if gitOutput(t, repository, "rev-parse", "HEAD") != beforeHead || gitOutput(t, repository, "symbolic-ref", "HEAD") != beforeBranch || gitOutput(t, repository, "remote", "get-url", "origin") != beforeRemote {
		t.Fatal("worktree lifecycle changed main HEAD, branch, or remote")
	}
	if gitOutput(t, record.Path, "rev-parse", "HEAD") != beforeHead {
		t.Fatal("worktree changes were automatically committed")
	}
	records, err := manager.List()
	if err != nil || len(records) != 1 || records[0].ID != record.ID {
		t.Fatalf("List = %#v, %v", records, err)
	}
	encoded, _ := json.Marshal(record.Public())
	if strings.Contains(string(encoded), repository) || strings.Contains(string(encoded), record.Path) || strings.Contains(string(encoded), "git_common_dir") {
		t.Fatalf("public worktree metadata leaks local path: %s", encoded)
	}
	removed, err := manager.Remove(record.ID)
	if err != nil || removed.ID != record.ID {
		t.Fatalf("Remove = %#v, %v", removed, err)
	}
	if _, err := os.Stat(record.Path); !os.IsNotExist(err) {
		t.Fatalf("worktree path still exists: %v", err)
	}
	if records, err = manager.List(); err != nil || len(records) != 0 {
		t.Fatalf("List after remove = %#v, %v", records, err)
	}
}

func TestConcurrentWritesUseIndependentDetachedWorktrees(t *testing.T) {
	repository := testRepository(t)
	manager, _ := New(t.TempDir())
	commit := gitOutput(t, repository, "rev-parse", "HEAD")
	records := make([]Record, 2)
	errorsByIndex := make([]error, 2)
	var group sync.WaitGroup
	for i := range records {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			records[index], errorsByIndex[index] = manager.Create(repository, "agt_writer", "req_concurrent_"+string(rune('a'+index)), commit)
		}(i)
	}
	group.Wait()
	for _, err := range errorsByIndex {
		if err != nil {
			t.Fatal(err)
		}
	}
	if records[0].ID == records[1].ID || records[0].Path == records[1].Path {
		t.Fatalf("concurrent worktrees share identity: %#v", records)
	}
	for _, record := range records {
		if gitOutput(t, record.Path, "rev-parse", "--abbrev-ref", "HEAD") != "HEAD" {
			t.Fatalf("worktree %s is not detached", record.ID)
		}
		if _, err := manager.Remove(record.ID); err != nil {
			t.Fatal(err)
		}
	}
}

func TestInvalidCommitCreatesNoWorktree(t *testing.T) {
	manager, _ := New(t.TempDir())
	if _, err := manager.Create(testRepository(t), "agt_writer", "req_invalid", "not-a-commit"); err == nil {
		t.Fatal("invalid commit was accepted")
	}
	records, err := manager.List()
	if err != nil || len(records) != 0 {
		t.Fatalf("records after invalid commit = %#v, %v", records, err)
	}
}

func testRepository(t *testing.T) string {
	t.Helper()
	repository := filepath.Join(t.TempDir(), "repository")
	if err := os.MkdirAll(repository, 0700); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repository, "init", "-b", "main")
	gitRun(t, repository, "config", "user.name", "PeerContext Test")
	gitRun(t, repository, "config", "user.email", "peerctx@example.invalid")
	if err := os.WriteFile(filepath.Join(repository, "fixture.txt"), []byte("main checkout\n"), 0600); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repository, "add", "fixture.txt")
	gitRun(t, repository, "commit", "-m", "fixture")
	gitRun(t, repository, "remote", "add", "origin", "https://example.invalid/private.git")
	return repository
}

func gitRun(t *testing.T, directory string, args ...string) {
	t.Helper()
	commandArgs := append([]string{"-C", directory}, args...)
	if output, err := exec.Command("git", commandArgs...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func gitOutput(t *testing.T, directory string, args ...string) string {
	t.Helper()
	commandArgs := append([]string{"-C", directory}, args...)
	output, err := exec.Command("git", commandArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}
