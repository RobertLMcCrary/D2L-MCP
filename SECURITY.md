# Security policy

## Reporting a vulnerability

Do not open a public issue containing tokens, cookies, student records, grades, private course content, or a working exploit. Contact the maintainers privately through the security-reporting channel configured on the repository.

Include:

- affected version and platform
- the smallest safe reproduction
- expected and observed behavior
- whether credentials or academic records may have been exposed

Revoke affected Brightspace sessions before sharing diagnostic material. Redact student names, identifiers, institution hostnames when sensitive, bearer tokens, cookies, and downloaded course content.

## Supported versions

Until the first stable release, security fixes are provided on the latest tagged release and the default branch.

## Operational guidance

- Keep `~/.d2l/token.json` and `~/.d2l/browser_profile` private.
- Use only HTTPS institution hosts.
- Run one interactive login at a time.
- Store downloaded course materials in a private directory.
- Review MCP client permissions before enabling download tools.
- Set `D2L_NO_AUTO_LOGIN=1` where browser-based silent refresh is inappropriate.

The server intentionally does not expose arbitrary Brightspace URLs or mutation endpoints.
