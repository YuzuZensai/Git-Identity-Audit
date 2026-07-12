package scanner

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

type Entry struct {
	Name          string
	Email         string
	Field         string // "Author", "Committer", or a trailer name like "Co-authored-by"
	Repo          string
	CommitSHA     string
	Date          string
	PushedByLogin string `json:",omitempty"` // GitHub login that pushed this commit, per the public Events API
}

const (
	unitSep   = "\x1f"
	recordSep = "\x1e"
)

var logFormat = "%H" + unitSep + "%an" + unitSep + "%ae" + unitSep + "%cn" + unitSep + "%ce" + unitSep + "%aI" + unitSep + "%B" + recordSep

var trailerPattern = regexp.MustCompile(`(?m)^([A-Za-z][A-Za-z-]*-by):\s*(.+?)\s*<([^>]+)>\s*$`)

func Clone(repoFullName, repoURL, token string) (dir string, cleanup func(), err error) {
	tmpDir, err := os.MkdirTemp("", "git-identity-audit-*")
	if err != nil {
		return "", nil, fmt.Errorf("creating temp dir: %w", err)
	}
	cleanup = func() { os.RemoveAll(tmpDir) }

	authURL := injectToken(repoURL, token)

	cmd := exec.Command("git", "clone", "--bare", "--filter=blob:none", "--quiet", authURL, tmpDir)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("cloning %s: %w: %s", repoFullName, err, stderr.String())
	}

	return tmpDir, cleanup, nil
}

func CountCommits(gitDir string) (int, error) {
	cmd := exec.Command("git", "--git-dir", gitDir, "rev-list", "--all", "--count")
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("counting commits: %w", err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0, fmt.Errorf("parsing commit count: %w", err)
	}
	return n, nil
}

func StreamLog(gitDir, repoFullName string, onCommit func(entries []Entry)) error {
	cmd := exec.Command("git", "--git-dir", gitDir, "log", "--all", "--format="+logFormat)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("opening log stream: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting log stream: %w", err)
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	scanner.Split(splitOnRecordSep)

	for scanner.Scan() {
		record := strings.Trim(scanner.Text(), "\n")
		if record == "" {
			continue
		}
		onCommit(parseRecord(record, repoFullName))
	}
	scanErr := scanner.Err()

	waitErr := cmd.Wait()
	if waitErr != nil {
		return fmt.Errorf("reading log for %s: %w: %s", repoFullName, waitErr, stderr.String())
	}
	if scanErr != nil {
		return fmt.Errorf("reading log for %s: %w", repoFullName, scanErr)
	}
	return nil
}

func splitOnRecordSep(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := bytes.IndexByte(data, recordSep[0]); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}

func injectToken(repoURL, token string) string {
	const prefix = "https://"
	if !strings.HasPrefix(repoURL, prefix) {
		return repoURL
	}
	return prefix + "x-access-token:" + token + "@" + strings.TrimPrefix(repoURL, prefix)
}

func parseRecord(record, repoFullName string) []Entry {
	fields := strings.SplitN(record, unitSep, 7)
	if len(fields) < 7 {
		return nil
	}
	sha, authorName, authorEmail, committerName, committerEmail, date, body := fields[0], fields[1], fields[2], fields[3], fields[4], fields[5], fields[6]

	entries := []Entry{
		{Name: authorName, Email: authorEmail, Field: "Author", Repo: repoFullName, CommitSHA: sha, Date: date},
		{Name: committerName, Email: committerEmail, Field: "Committer", Repo: repoFullName, CommitSHA: sha, Date: date},
	}
	for _, match := range trailerPattern.FindAllStringSubmatch(body, -1) {
		entries = append(entries, Entry{Name: match[2], Email: match[3], Field: match[1], Repo: repoFullName, CommitSHA: sha, Date: date})
	}
	return entries
}
