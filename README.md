# 🔍 Git Identity Audit

Scans every repository a GitHub personal access token can see and extracts
every contributor name/email identity found in commit metadata and
commit-message trailers (`Co-authored-by`, `Signed-off-by`, etc.), then
writes an interactive HTML report.

## Usage

```sh
go run . [-token <pat>] [-output <path>] [-concurrency <n>]
```

- `-token` - GitHub personal access token. Falls back to the `GITHUB_TOKEN`
  env var, then an interactive prompt.
- `-output` - path to write the HTML report (default
  `git-identity-audit-<unix-time>.html`).
- `-concurrency` - max number of repos to clone/scan in parallel (default `10`).

The token needs read access to whichever repos you want scanned (owned,
collaborator, and organization-member repos are all included).

## Build

```sh
go build ./...
```

## Releases

Prebuilt binaries for linux/darwin/windows (amd64+arm64) are published to
[GitHub Releases](https://github.com/YuzuZensai/Git-Identity-Audit/releases)
whenever a tag is pushed
