package scanner

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
)

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

func TestScanExtractsAuthorCommitterAndTrailers(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "author@example.com")
	runGit(t, dir, "config", "user.name", "Author One")

	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "file.txt")
	runGit(t, dir, "commit", "-q", "-m",
		"Initial commit\n\nCo-authored-by: Helper Two <helper@example.com>\nSigned-off-by: Author One <author@example.com>")

	runGit(t, dir, "-c", "user.name=Committer Three", "-c", "user.email=committer@example.com",
		"commit", "-q", "--allow-empty", "-m", "second commit")

	gitDir, cleanup, err := Clone("smoke/test", "file://"+dir, "")
	if err != nil {
		t.Fatalf("Clone failed: %v", err)
	}
	defer cleanup()

	count, err := CountCommits(gitDir)
	if err != nil {
		t.Fatalf("CountCommits failed: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 commits, got %d", count)
	}

	var entries []Entry
	commitsSeen := 0
	if err := StreamLog(gitDir, "smoke/test", func(e []Entry) {
		commitsSeen++
		entries = append(entries, e...)
	}); err != nil {
		t.Fatalf("StreamLog failed: %v", err)
	}
	if commitsSeen != 2 {
		t.Errorf("expected onCommit called 2 times, got %d", commitsSeen)
	}

	got := map[string]bool{}
	for _, e := range entries {
		got[e.Field+"|"+e.Name+"|"+e.Email] = true
	}

	want := []string{
		"Author|Author One|author@example.com",
		"Committer|Author One|author@example.com",
		"Co-authored-by|Helper Two|helper@example.com",
		"Signed-off-by|Author One|author@example.com",
		"Author|Author One|author@example.com",
		"Committer|Committer Three|committer@example.com",
	}
	for _, w := range want {
		if !got[w] {
			all := make([]string, 0, len(got))
			for k := range got {
				all = append(all, k)
			}
			sort.Strings(all)
			t.Errorf("expected entry %q not found; got: %v", w, all)
		}
	}
}
