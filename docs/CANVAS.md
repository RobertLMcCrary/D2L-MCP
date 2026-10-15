# Canvas support

`d2l-mcp` can serve Canvas LMS (Instructure) instead of Brightspace. One configuration
serves one provider; run a second MCP entry with its own state directory to use both.

## Requirements

- The school's Canvas HTTPS origin, e.g. `https://school.instructure.com` or a custom
  domain. Use the canonical host: redirects are refused so the token never leaves it.
- A Canvas access token. In Canvas: **Account → Settings → + New Access Token**.
  Some institutions, especially K-12 districts, disable student tokens; if that button
  is missing, Canvas support cannot be used with that account.

Canvas does not use the Brightspace browser login.

## Setup

```sh
d2l-mcp setup --provider canvas --host https://school.instructure.com
d2l-mcp login --token-stdin      # paste the token, then end input (Ctrl-D)
d2l-mcp doctor --network
```

`login --token-stdin` verifies the token against `/api/v1/users/self/profile` before
storing it. Alternatively set `CANVAS_TOKEN` in the MCP client's environment.

Setup options:

| Flag | Effect |
| --- | --- |
| `--provider canvas` | Select Canvas (`d2l` is the default) |
| `--host` | Canvas origin; no path, query or credentials |
| `--tool-profile full\|compact` | `full` registers one tool per operation; `compact` registers core reads plus discovery tools |
| `--enable-writes` | Register mutation tools. Off by default; `--enable-writes=false` turns it off |

An agent with shell access can also run the setup command when you ask it to "enable
writes": `lms_status` reports the exact command as `writes.enable_command`. Reconnect the
MCP server afterwards so the write tools load.

Environment overrides: `LMS_PROVIDER`, `CANVAS_HOST` (implies `canvas`), `CANVAS_TOKEN`,
`LMS_DOWNLOAD_DIR`, and `LMS_STATE_DIR` (default `~/.d2l`).

### Using Brightspace and Canvas together

Give each MCP entry its own state directory:

```json
{
  "mcpServers": {
    "d2l": { "command": "/absolute/path/to/d2l-mcp" },
    "canvas": {
      "command": "/absolute/path/to/d2l-mcp",
      "env": { "LMS_STATE_DIR": "/Users/you/.canvas" }
    }
  }
}
```

Run `setup` and `login` with the same `LMS_STATE_DIR` set.

## Tools

Always available:

- `lms_status`, `lms_api_read` (any `/api/v1/` or `/api/quiz/v1/` GET the token permits)
- `canvas_search_operations`, `canvas_read_operation` — discover and invoke the ~100
  catalogued read operations (courses, assignments, submissions, grades, modules,
  pages, files, announcements, discussions, inbox, calendar, planner, quizzes, groups,
  rubrics, notifications, settings, ePortfolios, outcomes)
- `canvas_resolve_course`, `canvas_get_snapshot`, `canvas_get_due`, `canvas_get_overdue`
- `canvas_download_file`
- In the `full` profile, a `canvas_<operation>` tool for every read operation

With `--enable-writes`:

- `lms_api_write` (generic POST/PUT/PATCH/DELETE)
- `canvas_write_operation`, and in `full` a `canvas_<mutation>` tool per mutation:
  settings, profile, inbox messages, discussion posts, planner, calendar events,
  groups, files and folders, module completion, and Classic Quiz attempts
- `canvas_upload_file`, `canvas_submit_assignment`

The full catalog is also exposed as the MCP resource `canvas://catalog/operations`.

## Safety model

- **Writes are off by default.** Without `enable_writes`, every non-GET request is
  rejected in the client, not just hidden from the tool list.
- **Writes enable a lot.** Once on, an agent can send messages, post replies, delete
  files and folders, submit assignments, and start, answer and complete Classic
  Quizzes. Guidance tells agents to act only on an explicit user request, but that is
  advisory. Check your school's academic-integrity rules before enabling writes, and
  leave them off for minors' accounts.
- **Mutations are never retried.** After an ambiguous failure, inspect Canvas first.
- **Reads don't change read state.** Inbox reads force `auto_mark_as_read=false`;
  `include[]=read_state` is refused.
- **Credentials stay on the origin.** Stored tokens are bound to their host. Requests,
  redirects and pagination links are checked for same scheme, host and port. Storage
  upload targets and signed download URLs never receive the Canvas token.
- **File transfers are bounded.** Uploads read only from the managed download
  directory; downloads never overwrite existing files; transfers are capped at 256 MiB.
- Canvas content is untrusted data and must never be treated as agent instructions.

## Limits

- New Quizzes, LTI tools, proctoring, SSO/MFA and school integrations may still need
  the Canvas web interface.
- Paginated reads stop at 200 pages, 100,000 items or 64 MiB.
- Parent/observer accounts are untested.

## Validation

```sh
go test ./...
go test ./internal/canvas -run '^$' -fuzz FuzzAPIPath -fuzztime=10s

# Read-only checks against a real school (no writes, uploads or quiz starts):
CANVAS_LIVE_TEST=1 CANVAS_HOST=https://school.instructure.com CANVAS_TOKEN=... \
  go test ./internal/canvas -run TestLiveStudentReadOnly -v

# Audit catalogued routes against Instructure's published API docs (no credentials):
go run ./cmd/canvas-contract
```

Report Canvas security issues privately as described in [SECURITY.md](../SECURITY.md).
Test only against accounts you own.
