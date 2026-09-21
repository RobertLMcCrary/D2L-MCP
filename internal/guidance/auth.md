# Authentication

Brightspace API bearer tokens expire frequently. The server reads the existing `~/.d2l/token.json` format used by `d2l-cli` and attempts a short headless refresh from `~/.d2l/browser_profile` when needed.

If the saved browser session has expired, run:

```sh
d2l-mcp login
```

The command opens Chrome for interactive SSO and stores the captured API token with mode `0600`. Browser automation is used only for authentication. Set `D2L_NO_AUTO_LOGIN=1` to disable silent refresh.

Never place tokens in logs, tool results, source files, or support messages.
