# D2L MCP

[![CI](https://github.com/RobertLMcCrary/D2L-MCP/actions/workflows/ci.yml/badge.svg)](https://github.com/RobertLMcCrary/D2L-MCP/actions/workflows/ci.yml)
[![Latest release](https://img.shields.io/github/v/release/RobertLMcCrary/D2L-MCP?include_prereleases&sort=semver)](https://github.com/RobertLMcCrary/D2L-MCP/releases)
[![Go version](https://img.shields.io/github/go-mod/go-version/RobertLMcCrary/D2L-MCP)](go.mod)
[![License](https://img.shields.io/github/license/RobertLMcCrary/D2L-MCP)](LICENSE)

A local [Model Context Protocol](https://modelcontextprotocol.io/) server for read-only D2L Brightspace academic data. It runs over stdio and is written in Go with [mark3labs/mcp-go](https://github.com/mark3labs/mcp-go).

> [!IMPORTANT]
> D2L MCP is unofficial. It is not affiliated with or endorsed by D2L Corporation or any educational institution.

## Features

- Courses, identity, grades, assignments, quizzes, discussions, and announcements
- Calendar, due and overdue work, recent updates, and course snapshots
- Course-content browsing by root, table of contents, or module
- Individual and recursive downloads for course materials
- Assignment-attachment downloads
- Public SimpleSyllabus retrieval
- Browser-assisted Brightspace login and silent token refresh
- Typed MCP inputs, structured outputs, guidance resources, and workflow prompts

Brightspace academic access is GET-only. Authentication performs a token-exchange POST inside the user's authenticated browser session. Downloads write only to a managed local directory.

## Project status

The project is pre-1.0 and currently targets behavioral compatibility with [`d2l-cli` v0.2.2](https://github.com/Aaryan-Kapoor/d2l-cli/tree/v0.2.2). Interfaces may change before v1.0, but tagged releases follow semantic versioning.

See [docs/PARITY.md](docs/PARITY.md) for the complete command mapping, deliberate MCP interface differences, and upstream source citations.

## Install

### Download a release

Download the archive for your platform from [GitHub Releases](https://github.com/RobertLMcCrary/D2L-MCP/releases):

- `darwin_arm64` for Apple silicon
- `darwin_amd64` for Intel Macs
- `linux_arm64` or `linux_amd64` for Linux
- `windows_arm64` or `windows_amd64` for Windows

Each release includes platform archives and `checksums.txt`. Verify the archive checksum, extract the binary, and place `d2l-mcp` (or `d2l-mcp.exe`) somewhere on `PATH`.

Release binaries are not currently code-signed or notarized. Your operating system may require confirmation before first launch.

### Build from source

Building requires Go 1.25.5 or newer:

```sh
git clone https://github.com/RobertLMcCrary/D2L-MCP.git
cd D2L-MCP
go build -trimpath -o d2l-mcp ./cmd/d2l-mcp
```

Confirm the installation:

```sh
./d2l-mcp version
./d2l-mcp --help
```

Source builds report `dev`; tagged release binaries report their release version. Move the built executable to a directory on `PATH` before continuing.

## Configure and authenticate

Runtime requirements:

- A Brightspace account
- Google Chrome, Chromium, or another Chrome-compatible executable discoverable by `chromedp`

Use a known school preset:

```sh
d2l-mcp setup --school ksu
d2l-mcp setup --school gsu
```

Or configure any Brightspace host:

```sh
d2l-mcp setup \
  --host https://your-school.brightspace.example \
  --syllabus-host https://your-school.simplesyllabus.com
```

Authenticate and verify the setup:

```sh
d2l-mcp login
d2l-mcp doctor
```

State remains compatible with `d2l-cli` under `~/.d2l/`:

- `config.json`
- `token.json`
- `browser_profile/`

Tokens and browser state are secrets. Never commit or share them.

## MCP client configuration

Configure your MCP client with the binary's absolute path:

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

## MCP tools

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

## Security

- Academic API calls are GET-only.
- HTTPS Brightspace hosts are required.
- Bearer credentials are not forwarded across host redirects.
- Tokens use mode `0600`; state directories use `0700` on supported systems.
- Browser refresh is cancellable and serialized with a lock file.
- Requests have deadlines, bounded pagination, and context-aware rate-limit retries.
- Downloads have a 256 MiB per-file limit and reject traversal, symlink roots, and overwrites.
- LMS text and files are treated as untrusted data, not agent instructions.

Read [SECURITY.md](SECURITY.md) before reporting a vulnerability or sharing diagnostics.

## Development

```sh
go test ./...
go test -race ./...
go vet ./...
go test ./internal/d2l -run '^$' -fuzz FuzzSafeFilename -fuzztime=10s
```

Validate the complete cross-platform release locally with [GoReleaser](https://goreleaser.com/):

```sh
goreleaser check
goreleaser release --snapshot --clean
```

The test suite covers fixture-backed API behavior, pagination, retries, cancellation, authentication-state compatibility, path security, in-process MCP negotiation, and a true stdio subprocess.

Contributions are welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md) for safety requirements, development checks, and fixture-sanitization rules.

## Attribution

D2L MCP is a native Go adaptation of [Aaryan Kapoor's `d2l-cli`](https://github.com/Aaryan-Kapoor/d2l-cli). Aaryan designed and implemented the original CLI, endpoint coverage, authentication flow, course resolution, SimpleSyllabus integration, downloads, snapshot workflow, and agent guidance that made this port possible.

Detailed derivation and source citations are preserved in [NOTICE](NOTICE) and [docs/PARITY.md](docs/PARITY.md).

## License

D2L MCP and its upstream-derived portions are available under the [MIT License](LICENSE). The original `d2l-cli` copyright and permission notice are retained.
