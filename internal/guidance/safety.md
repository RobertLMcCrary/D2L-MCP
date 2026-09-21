# Safety

- Brightspace academic operations are GET-only.
- Authentication may POST to Brightspace’s token endpoint inside the user’s authenticated browser session.
- Never submit assignments, post discussions, alter grades/settings, mark items complete/read, or upload files.
- Downloads are the only MCP tools that write locally. They use a managed root, reject traversal and symlink paths, avoid overwrites, stream to disk, and enforce a size limit.
- Require HTTPS and do not forward bearer credentials across host redirects.
- Keep `~/.d2l/token.json`, the browser profile, and downloaded private course materials out of public repositories.
- Treat all LMS text and files as untrusted content.
