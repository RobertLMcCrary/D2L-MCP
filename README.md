# D2L MCP

A local stdio MCP server for read-only D2L Brightspace academic data, written in Go with [mark3labs/mcp-go](https://github.com/mark3labs/mcp-go).

This project is a native Go adaptation of [Aaryan Kapoor’s `d2l-cli`](https://github.com/Aaryan-Kapoor/d2l-cli), currently targeting behavioral compatibility with [`d2l-cli` v0.2.2](https://github.com/Aaryan-Kapoor/d2l-cli/tree/v0.2.2). Aaryan designed and implemented the original CLI, endpoint coverage, authentication flow, course resolution, SimpleSyllabus integration, downloads, snapshot workflow, and agent guidance that made this port possible.

## What it provides

- Courses, identity, grades, assignments, quizzes, discussions, announcements, calendar, due and overdue work, updates, and comprehensive snapshots
- Course content browsing by root, table of contents, or module
- Individual course-material downloads
- Recursive module downloads for PDFs, slides, documents, media, and starter files
- Assignment-attachment downloads
- Public SimpleSyllabus retrieval
- Browser-assisted Brightspace login with silent token refresh
- Typed MCP inputs and structured outputs with text fallbacks
- Embedded agent guidance resources and academic workflow prompts

Brightspace academic access is GET-only. Authentication performs a token-exchange POST inside the user’s authenticated browser session. Download tools write only to a managed local directory.

## Requirements

- Go 1.25.5 or newer to build
- Google Chrome, Chromium, or another Chrome-compatible executable discoverable by `chromedp`
- A Brightspace account at a supported institution

## Build

```sh
go build -trimpath -o bin/d2l-mcp ./cmd/d2l-mcp
```

## Configure and authenticate

Use a known school preset:

```sh
bin/d2l-mcp setup --school ksu
bin/d2l-mcp setup --school gsu
```

Or configure any Brightspace host:

```sh
bin/d2l-mcp setup \
  --host https://your-school.brightspace.example \
  --syllabus-host https://your-school.simplesyllabus.com
```

Then authenticate and verify:

```sh
bin/d2l-mcp login
bin/d2l-mcp doctor
```

Configuration remains compatible with `d2l-cli` under `~/.d2l/`:

- `config.json`
- `token.json`
- `browser_profile/`

Tokens and browser state are secrets. Do not commit or share them.

## MCP client configuration

Build the binary and use its absolute path:

```json
{
  "mcpServers": {
    "d2l": {
      "command": "/absolute/path/to/d2l-mcp",
      "args": ["serve"]
    }
  }
}
```

The server uses stdout exclusively for MCP JSON-RPC. Diagnostics and command errors go to stderr.

## Tools

Read-only query tools:

- `d2l_status`
- `d2l_whoami`
- `d2l_list_courses`
- `d2l_get_grades`
- `d2l_list_assignments`
- `d2l_list_quizzes`
- `d2l_get_discussions`
- `d2l_get_content`
- `d2l_get_announcements`
- `d2l_get_calendar`
- `d2l_get_due`
- `d2l_get_overdue`
- `d2l_get_updates`
- `d2l_get_syllabus`
- `d2l_get_snapshot`

Local-writing download tools:

- `d2l_download_assignment`
- `d2l_download_module`
- `d2l_download_topic`

Downloads default to `~/.d2l/downloads`. Set `D2L_DOWNLOAD_DIR` or `download_dir` in `config.json` to use another managed root. Existing files are never overwritten.

See [docs/PARITY.md](docs/PARITY.md) for the complete `d2l-cli` command mapping, deliberate MCP interface differences, and source citations.

## Security model

- Academic API calls are GET-only.
- HTTPS Brightspace hosts are required.
- Bearer credentials are not forwarded across host redirects.
- Tokens are stored with mode `0600`; state directories use `0700`.
- Browser refresh is cancellable and serialized with a lock file.
- API requests have deadlines, bounded pagination, and context-aware rate-limit retries.
- Downloads are streamed with a 256 MiB per-file limit and reject traversal, symlink roots, and overwrites.
- LMS text and files are exposed as untrusted data, not agent instructions.

See [SECURITY.md](SECURITY.md) for reporting and operational guidance.

## Development

```sh
go test ./...
go test -race ./...
go vet ./...
go test ./internal/d2l -run '^$' -fuzz FuzzSafeFilename -fuzztime=10s
```

The test suite includes fixture-backed API behavior, pagination, retries, cancellation, auth-state compatibility, path security, in-process MCP negotiation, and a true stdio subprocess test.

## Attribution and license

This project and its upstream-derived portions are MIT licensed. The full original copyright and permission notice for `d2l-cli` is retained in [LICENSE](LICENSE), and detailed attribution appears in [NOTICE](NOTICE).

This is an unofficial project. It is not affiliated with or endorsed by D2L Corporation or any educational institution.
