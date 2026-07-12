package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/YuzuZensai/Git-Identity-Audit/internal/github"
	"github.com/YuzuZensai/Git-Identity-Audit/internal/report"
	"github.com/YuzuZensai/Git-Identity-Audit/internal/scanner"
	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
	"golang.org/x/term"
)

var overallBarStyle = mpb.BarStyle().Lbound("[").Filler("=").Tip(">").Padding("-").Rbound("]")

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	tokenFlag := flag.String("token", "", "GitHub personal access token (prefer GITHUB_TOKEN env var or the interactive prompt instead)")
	output := flag.String("output", fmt.Sprintf("git-identity-audit-%d.html", time.Now().Unix()), "path to write the HTML report")
	concurrency := flag.Int("concurrency", 10, "max number of repos to clone/scan in parallel")
	flag.Parse()

	token, err := resolveToken(*tokenFlag)
	if err != nil {
		return err
	}

	client := github.NewClient(token)

	fmt.Println("Discovering accessible repositories...")
	repos, err := client.ListAccessibleRepos()
	if err != nil {
		return fmt.Errorf("listing repositories: %w", err)
	}
	fmt.Printf("Found %d repositories.\n", len(repos))

	entries, scanned, failed, warnings := scanAll(repos, token, *concurrency)
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, w)
	}
	fmt.Printf("Scanned %d/%d repos successfully (%d failed).\n", scanned, len(repos), failed)

	events, err := fetchEvents(client)
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: skipping GitHub events correlation:", err)
	}
	correlateEvents(entries, events)

	if err := report.WriteHTML(*output, entries, events); err != nil {
		return fmt.Errorf("writing HTML report: %w", err)
	}
	fmt.Printf("Wrote %d identity entries to %s\n", len(entries), *output)

	report.PrintSummary(entries)
	return nil
}

func resolveToken(flagToken string) (string, error) {
	if flagToken != "" {
		return flagToken, nil
	}
	if envToken := os.Getenv("GITHUB_TOKEN"); envToken != "" {
		return envToken, nil
	}
	fmt.Print("GitHub PAT: ")
	tokenBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("reading token: %w", err)
	}
	token := strings.TrimSpace(string(tokenBytes))
	if token == "" {
		return "", fmt.Errorf("no token provided")
	}
	return token, nil
}

func fetchEvents(client *github.Client) ([]github.Event, error) {
	login, err := client.CurrentUserLogin()
	if err != nil {
		return nil, fmt.Errorf("resolving token owner: %w", err)
	}
	events, err := client.PublicEvents(login)
	if err != nil {
		return nil, fmt.Errorf("fetching public events for %s: %w", login, err)
	}
	return events, nil
}

func correlateEvents(entries []scanner.Entry, events []github.Event) {
	heads := make(map[string]string, len(events))
	for _, e := range events {
		if e.Type != "PushEvent" || e.Payload.Head == "" {
			continue
		}
		heads[e.Repo.Name+"@"+e.Payload.Head] = e.Actor.Login
	}
	for i := range entries {
		if entries[i].Field != "Committer" {
			continue
		}
		if login, ok := heads[entries[i].Repo+"@"+entries[i].CommitSHA]; ok {
			entries[i].PushedByLogin = login
		}
	}
}

func scanAll(repos []github.Repo, token string, concurrency int) ([]scanner.Entry, int, int, []string) {
	var (
		mu       sync.Mutex
		entries  []scanner.Entry
		scanned  int
		failed   int
		warnings []string
		wg       sync.WaitGroup
		sem      = make(chan struct{}, concurrency)
	)

	progress := mpb.New(mpb.WithWidth(50))
	overallBar := progress.New(int64(len(repos)), overallBarStyle,
		mpb.BarPriority(math.MaxInt),
		mpb.PrependDecorators(decor.Name("total repos", decor.WC{W: 14})),
		mpb.AppendDecorators(decor.CountersNoUnit("%d / %d"), decor.Name(" "), decor.Percentage(decor.WC{W: 5})),
	)

	for _, repo := range repos {
		repo := repo
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			defer overallBar.Increment()

			err := scanRepo(progress, repo, token, &mu, &entries)

			mu.Lock()
			if err != nil {
				failed++
				warnings = append(warnings, fmt.Sprintf("warning: skipping %s: %v", repo.FullName, err))
			} else {
				scanned++
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	progress.Wait()

	return entries, scanned, failed, warnings
}

func scanRepo(progress *mpb.Progress, repo github.Repo, token string, mu *sync.Mutex, entries *[]scanner.Entry) error {
	gitDir, cleanup, err := scanner.Clone(repo.FullName, repo.CloneURL, token)
	if err != nil {
		return err
	}
	defer cleanup()

	count, err := scanner.CountCommits(gitDir)
	if err != nil || count == 0 {
		count = 1
	}

	repoBar := progress.New(int64(count), overallBarStyle,
		mpb.PrependDecorators(decor.Name(repo.FullName, decor.WCSyncSpaceR)),
		mpb.AppendDecorators(decor.CountersNoUnit("%d / %d commits")),
	)

	err = scanner.StreamLog(gitDir, repo.FullName, func(e []scanner.Entry) {
		mu.Lock()
		*entries = append(*entries, e...)
		mu.Unlock()
		repoBar.Increment()
	})
	repoBar.SetCurrent(int64(count))
	return err
}
