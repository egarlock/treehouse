package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepoRootFromCommonGitDirHandlesForwardSlashPath(t *testing.T) {
	root, ok := repoRootFromCommonGitDir("C:/Users/runner/AppData/Local/Temp/repo/.git")
	if !ok {
		t.Fatal("expected .git common dir to resolve to a repo root")
	}

	want := filepath.Clean(filepath.FromSlash("C:/Users/runner/AppData/Local/Temp/repo"))
	if root != want {
		t.Fatalf("expected repo root %q, got %q", want, root)
	}
}

func TestGetDefaultBranchFromDetachedLinkedWorktreeUsesMainRepoHead(t *testing.T) {
	base := t.TempDir()
	repoDir := filepath.Join(base, "repo")
	wtPath := filepath.Join(base, "worktree")

	mustGit(t, "", "init", "--initial-branch=main", repoDir)
	mustGit(t, repoDir, "config", "user.email", "test@test.com")
	mustGit(t, repoDir, "config", "user.name", "Test")
	mustGit(t, repoDir, "config", "init.defaultBranch", "wrong")
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repoDir, "add", ".")
	mustGit(t, repoDir, "commit", "-m", "initial")
	mustGit(t, repoDir, "worktree", "add", "--detach", wtPath, "main")

	branch, err := GetDefaultBranch(wtPath)
	if err != nil {
		t.Fatalf("GetDefaultBranch failed: %v", err)
	}
	if branch != "main" {
		t.Fatalf("expected default branch main from main repo HEAD, got %q", branch)
	}
}

func TestFindMainRepoRootFromLinkedWorktree(t *testing.T) {
	base := t.TempDir()
	base, err := filepath.EvalSymlinks(base)
	if err != nil {
		t.Fatal(err)
	}
	repoDir := filepath.Join(base, "repo")
	wtPath := filepath.Join(base, "worktree")

	mustGit(t, "", "init", "--initial-branch=main", repoDir)
	mustGit(t, repoDir, "config", "user.email", "test@test.com")
	mustGit(t, repoDir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repoDir, "add", ".")
	mustGit(t, repoDir, "commit", "-m", "initial")
	mustGit(t, repoDir, "worktree", "add", "--detach", wtPath, "main")

	mainRoot, err := FindMainRepoRootFrom(wtPath)
	if err != nil {
		t.Fatalf("FindMainRepoRootFrom failed: %v", err)
	}
	if mainRoot != repoDir {
		t.Fatalf("expected main repo root %s, got %s", repoDir, mainRoot)
	}
}

func TestRemoveCleanWorktreeRejectsDirtyWorktree(t *testing.T) {
	base := t.TempDir()
	repoDir := filepath.Join(base, "repo")
	wtPath := filepath.Join(base, "worktree")

	mustGit(t, "", "init", "--initial-branch=main", repoDir)
	mustGit(t, repoDir, "config", "user.email", "test@test.com")
	mustGit(t, repoDir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repoDir, "add", ".")
	mustGit(t, repoDir, "commit", "-m", "initial")
	mustGit(t, repoDir, "worktree", "add", "--detach", wtPath, "main")

	dirtyPath := filepath.Join(wtPath, "uncommitted.txt")
	if err := os.WriteFile(dirtyPath, []byte("keep me\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := RemoveCleanWorktree(repoDir, wtPath); err == nil {
		t.Fatal("expected clean worktree removal to reject dirty worktree")
	}
	if _, err := os.Stat(dirtyPath); err != nil {
		t.Fatalf("expected dirty worktree to remain: %v", err)
	}
}

func TestFetchOnlyUpdatesDefaultBranch(t *testing.T) {
	base := t.TempDir()
	originDir := filepath.Join(base, "origin")
	cloneDir := filepath.Join(base, "clone")

	// Origin repo with two branches: main (default) and feature.
	mustGit(t, "", "init", "--initial-branch=main", originDir)
	mustGit(t, originDir, "config", "user.email", "test@test.com")
	mustGit(t, originDir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(originDir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, originDir, "add", ".")
	mustGit(t, originDir, "commit", "-m", "initial")
	mustGit(t, originDir, "branch", "feature")

	// Clone it: origin is set with the default all-branches refspec and both
	// remote-tracking branches exist. origin/HEAD points at main.
	mustGit(t, "", "clone", originDir, cloneDir)
	mustGit(t, cloneDir, "config", "user.email", "test@test.com")
	mustGit(t, cloneDir, "config", "user.name", "Test")

	featureBefore := mustGitOut(t, cloneDir, "rev-parse", "refs/remotes/origin/feature")

	// Advance both branches on origin AFTER the clone.
	mustGit(t, originDir, "checkout", "feature")
	if err := os.WriteFile(filepath.Join(originDir, "feature.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, originDir, "add", ".")
	mustGit(t, originDir, "commit", "-m", "feature advance")
	featureNew := mustGitOut(t, originDir, "rev-parse", "HEAD")

	mustGit(t, originDir, "checkout", "main")
	if err := os.WriteFile(filepath.Join(originDir, "main.txt"), []byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, originDir, "add", ".")
	mustGit(t, originDir, "commit", "-m", "main advance")
	mainNew := mustGitOut(t, originDir, "rev-parse", "HEAD")

	// Fetch should update only origin/main, not origin/feature, even though the
	// clone's configured refspec covers all branches.
	if err := Fetch(cloneDir); err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if got := mustGitOut(t, cloneDir, "rev-parse", "refs/remotes/origin/main"); got != mainNew {
		t.Fatalf("expected origin/main updated to %s, got %s", mainNew, got)
	}
	if got := mustGitOut(t, cloneDir, "rev-parse", "refs/remotes/origin/feature"); got != featureBefore {
		t.Fatalf("expected origin/feature unchanged at %s, got %s", featureBefore, got)
	}
	// The advanced feature commit must NOT have been downloaded by a scoped fetch.
	if _, err := runGit(cloneDir, "cat-file", "-e", featureNew); err == nil {
		t.Fatalf("expected feature commit %s to be absent after scoped fetch", featureNew)
	}
}

func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func mustGitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}
