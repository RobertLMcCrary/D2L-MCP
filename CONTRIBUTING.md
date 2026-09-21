# Contributing

Contributions are welcome, especially sanitized Brightspace response fixtures from institutions whose payload shapes differ from the existing tests.

## Ground rules

- Preserve remote read-only behavior. Do not add submission, posting, grading, upload, completion, or settings mutations.
- Never commit tokens, cookies, student records, grades, private announcements, or course files.
- Keep stdout protocol-clean when `serve` is running.
- Add typed MCP schemas and stable error codes for new tools.
- Treat LMS text and HTML as untrusted data.
- Cite upstream `d2l-cli` source when porting a non-obvious algorithm or authentication behavior.

## Before opening a change

```sh
gofmt -w .
go mod tidy
go vet ./...
go test -race ./...
go test ./internal/d2l -run '^$' -fuzz FuzzSafeFilename -fuzztime=10s
```

For protocol changes, build the binary and run the stdio integration test:

```sh
go build -o /tmp/d2l-mcp ./cmd/d2l-mcp
D2L_MCP_TEST_BINARY=/tmp/d2l-mcp go test ./internal/mcpserver -run TestStdioSubprocess
```

## Fixtures

Remove names, identifiers, course titles, institution-specific secrets, URLs with sensitive query strings, and free-form content before committing fixtures. Preserve only fields needed to demonstrate the payload shape or bug.
