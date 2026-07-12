package report

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"sort"
	"time"

	"github.com/YuzuZensai/Git-Identity-Audit/internal/github"
	"github.com/YuzuZensai/Git-Identity-Audit/internal/scanner"
)

//go:embed template.html
var templateHTML string

var htmlTemplate = template.Must(template.New("report").Parse(templateHTML))

func WriteHTML(path string, entries []scanner.Entry, events []github.Event) error {
	dataJSON, err := json.Marshal(entries)
	if err != nil {
		return fmt.Errorf("marshaling entries: %w", err)
	}
	eventsJSON, err := json.Marshal(events)
	if err != nil {
		return fmt.Errorf("marshaling events: %w", err)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating %s: %w", path, err)
	}
	defer f.Close()

	return htmlTemplate.Execute(f, struct {
		DataJSON    template.JS
		EventsJSON  template.JS
		GeneratedAt string
	}{
		DataJSON:    template.JS(dataJSON),
		EventsJSON:  template.JS(eventsJSON),
		GeneratedAt: time.Now().Format(time.RFC1123),
	})
}

type summaryRow struct {
	Name       string
	Email      string
	Count      int
	RepoCounts map[string]int
}

func PrintSummary(entries []scanner.Entry) {
	rows := map[string]*summaryRow{}
	for _, e := range entries {
		key := e.Name + "\x00" + e.Email
		row, ok := rows[key]
		if !ok {
			row = &summaryRow{Name: e.Name, Email: e.Email, RepoCounts: map[string]int{}}
			rows[key] = row
		}
		row.Count++
		row.RepoCounts[e.Repo]++
	}

	sorted := make([]*summaryRow, 0, len(rows))
	for _, r := range rows {
		sorted = append(sorted, r)
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Count > sorted[j].Count })

	fmt.Printf("\n%d unique identities found:\n\n", len(sorted))
	for i, r := range sorted {
		repoNames := make([]string, 0, len(r.RepoCounts))
		for repo := range r.RepoCounts {
			repoNames = append(repoNames, repo)
		}
		sort.Strings(repoNames)

		fmt.Printf("%3d. %s <%s>\n", i+1, r.Name, r.Email)
		fmt.Printf("     %d commits across %d repo(s):\n", r.Count, len(repoNames))
		for _, repo := range repoNames {
			fmt.Printf("       - %s (%d)\n", repo, r.RepoCounts[repo])
		}
		fmt.Println()
	}
}
