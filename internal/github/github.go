package github

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const apiBase = "https://api.github.com"

type Client struct {
	token string
	http  *http.Client
}

func NewClient(token string) *Client {
	return &Client{
		token: token,
		http:  &http.Client{Timeout: 30 * time.Second},
	}
}

type Repo struct {
	FullName string `json:"full_name"`
	CloneURL string `json:"clone_url"`
	Private  bool   `json:"private"`
}

type Event struct {
	Type      string `json:"type"`
	CreatedAt string `json:"created_at"`
	Actor     struct {
		Login string `json:"login"`
	} `json:"actor"`
	Repo struct {
		Name string `json:"name"`
	} `json:"repo"`
	Payload struct {
		Ref    string `json:"ref"`
		Head   string `json:"head"`
		Before string `json:"before"`
	} `json:"payload"`
}

func (c *Client) CurrentUserLogin() (string, error) {
	var user struct {
		Login string `json:"login"`
	}
	if _, err := c.get(apiBase+"/user", &user); err != nil {
		return "", err
	}
	return user.Login, nil
}

func (c *Client) PublicEvents(login string) ([]Event, error) {
	var all []Event
	url := apiBase + "/users/" + login + "/events/public?per_page=100"
	for url != "" {
		var page []Event
		next, err := c.get(url, &page)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		url = next
	}
	return all, nil
}

func (c *Client) ListAccessibleRepos() ([]Repo, error) {
	var all []Repo
	url := apiBase + "/user/repos?affiliation=owner,collaborator,organization_member&per_page=100"
	for url != "" {
		var page []Repo
		next, err := c.get(url, &page)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		url = next
	}
	return all, nil
}

func (c *Client) get(url string, out any) (string, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0" {
		reset := resp.Header.Get("X-RateLimit-Reset")
		return "", fmt.Errorf("rate limited by GitHub API, resets at unix time %s", reset)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API request to %s failed: %s", url, resp.Status)
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return "", fmt.Errorf("decoding response from %s: %w", url, err)
	}

	return parseNextLink(resp.Header.Get("Link")), nil
}

func parseNextLink(header string) string {
	if header == "" {
		return ""
	}
	for _, part := range strings.Split(header, ",") {
		segments := strings.Split(part, ";")
		if len(segments) < 2 {
			continue
		}
		if strings.TrimSpace(segments[1]) == `rel="next"` {
			url := strings.TrimSpace(segments[0])
			return strings.Trim(url, "<>")
		}
	}
	return ""
}
