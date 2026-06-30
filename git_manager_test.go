package watch

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func runGitForTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

func TestGitCommandsScopeToDirWithoutChangingCwd(t *testing.T) {
	before, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	if c := gitDirCommand("/tmp/repo", "status"); c.Dir != "/tmp/repo" {
		t.Fatalf("gitDirCommand Dir = %q, want /tmp/repo", c.Dir)
	}
	if c := gitCommand("/tmp/repo", "https://example.com/x.git", "u", "tok", "fetch", "origin"); c.Dir != "/tmp/repo" {
		t.Fatalf("gitCommand Dir = %q, want /tmp/repo", c.Dir)
	}

	if after, _ := os.Getwd(); after != before {
		t.Fatalf("building git commands changed cwd: %q != %q", after, before)
	}
}

// The git sync runs concurrently with DuckDB writes that use paths anchored at
// startup; if any git operation changed the process-wide working directory the
// DuckDB WAL path would break. This guards that invariant end to end.
func TestGitManagerDoesNotMutateWorkingDir(t *testing.T) {
	if !IsGitInstalled() {
		t.Skip("git not installed")
	}

	origin := filepath.Join(t.TempDir(), "origin")
	runGitForTest(t, "", "init", "-b", "main", origin)
	if err := os.WriteFile(filepath.Join(origin, "rule.ws"), []byte("// rule\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGitForTest(t, origin, "add", ".")
	runGitForTest(t, origin, "commit", "-m", "seed")

	local := filepath.Join(t.TempDir(), "clone")
	gm := NewGitManager(origin, "main", local, "", "")

	before, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	if err := gm.CloneOrUpdate(); err != nil {
		t.Fatalf("clone: %v", err)
	}
	if err := gm.updateRepository(); err != nil {
		t.Fatalf("update: %v", err)
	}
	if _, err := gm.GetCurrentCommit(); err != nil {
		t.Fatalf("current commit: %v", err)
	}
	_ = gm.GetRepositoryInfo()

	after, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("git operations changed the working directory: before=%q after=%q", before, after)
	}
}
