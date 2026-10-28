# jira-mcp

Local Jira MCP server for AI agents. Four tools, secure by default, minimal
context overhead — with batch worklog logging and Server/Data Center support
built in.

## Tools

| Tool | What it does |
|---|---|
| `jira_read` | Fetch issues by key, search with JQL, list projects/boards/sprints, remote links, worklogs |
| `jira_write` | Create, update, delete, transition, comment, link, remote link, set parent, log work — Markdown by default, `dry_run` supported |
| `jira_schema` | Discover fields, transitions, link types, and allowed values |
| `jira_user_search` | Resolve names and emails to account IDs for assignment |

## Install

```bash
go build -o jira-mcp ./cmd/jira-mcp      # from source (Go 1.27+)
# or
docker build -t jira-mcp .
```

Register with an MCP client:

```bash
claude mcp add-json jira '{
  "command": "/usr/local/bin/jira-mcp",
  "env": {
    "JIRA_URL": "https://yourcompany.atlassian.net",
    "JIRA_EMAIL": "you@company.com",
    "JIRA_API_TOKEN": "your-api-token"
  }
}'
```

## Configuration

| Variable | Purpose |
|---|---|
| `JIRA_URL` | Jira base URL |
| `JIRA_EMAIL` + `JIRA_API_TOKEN` | Jira Cloud (Basic auth) |
| `JIRA_USER` + `JIRA_PASS` | Server/Data Center (Basic auth) |
| `JIRA_PERSONAL_TOKEN` | Server/Data Center Personal Access Token (Bearer) |
| `JIRA_API_VERSION` | `3` (default, Cloud/ADF) or `2` (Server/Data Center compat mode) |

## Batch worklog

Log 8h on every working day in a range in one call — dates, weekends and
timezones are computed server-side, deterministically, not by the model:

```json
{"action": "worklog", "items": [{"key": "PROJ-1", "worklog": {
  "from_date": "2026-07-20", "to_date": "2026-07-24",
  "hours_per_day": 8, "start_time": "08:00", "tz_offset": "+0700",
  "skip_existing": true, "comment": "Log tu dong"
}}]}
```

Weekends are skipped by default (`skip_weekends: false` includes them),
`hours_per_day` defaults to 8, `start_time` to 08:00. `skip_existing: true`
makes reruns idempotent; `dry_run: true` previews the plan. Single entry:
`{"started": "2026-07-20T08:00:00.000+0700", "time_spent_seconds": 28800}`.
Read entries back with `jira_read {"resource": "worklogs", "issue_key": "PROJ-1"}`.

## API v2 compatibility mode

On Server/Data Center instances without the v3 API, set `JIRA_API_VERSION=2`:
schema discovery switches to the legacy `createmeta` document, search to the
v2 `search` endpoint, and descriptions/comments are sent as plain strings
instead of ADF. Worklogs always use the v2 endpoint, so plain-text comments
work on both Cloud and Data Center.

## License

MIT — see [LICENSE](LICENSE).
