# d2l-cli v0.2.2 parity

This server ports the academic behavior of [Aaryan Kapoor’s `d2l-cli` v0.2.2](https://github.com/Aaryan-Kapoor/d2l-cli/tree/v0.2.2) to typed MCP tools.

## Academic command mapping

- `whoami` → `d2l_whoami`
- `courses` → `d2l_list_courses`
- `grades` and `grades --final` → `d2l_get_grades`
- `assignments` → `d2l_list_assignments`
- `quizzes` → `d2l_list_quizzes`
- `discussions` → `d2l_get_discussions`
- `content` and `content --toc` → `d2l_get_content`
- `news` → `d2l_get_announcements`
- `calendar` → `d2l_get_calendar`
- `due` → `d2l_get_due`
- `overdue` → `d2l_get_overdue`
- `updates` → `d2l_get_updates`
- `syllabus` → `d2l_get_syllabus`
- `dump` → `d2l_get_snapshot`
- `download` → `d2l_download_assignment`
- `download-content` → `d2l_download_module` and `d2l_download_topic`

The endpoint paths and API versions derive from the upstream [read-only client](https://github.com/Aaryan-Kapoor/d2l-cli/blob/v0.2.2/src/d2l/client.py).

## Deliberate interface differences

- Output is typed `structuredContent` with a JSON text fallback instead of terminal tables, global `--json`, or global `--md`.
- Interactive `setup`, `login`, and `doctor` remain standalone subcommands because an MCP stdio request is a poor place to block on browser SSO.
- `onboard` is represented by the `course_onboarding` MCP prompt and guidance resources so the host agent conducts the interview.
- `skill install` and `skill cat` are replaced by embedded MCP resources.
- Self-update is delegated to package managers and release artifacts rather than allowing the MCP server to execute Git or pip.
- Downloads use a managed root, stream to disk, enforce a size limit, reject symlinks/traversal, and do not overwrite files.

## Compatibility sources

- [CLI registration](https://github.com/Aaryan-Kapoor/d2l-cli/blob/v0.2.2/src/d2l/cli.py)
- [Brightspace client](https://github.com/Aaryan-Kapoor/d2l-cli/blob/v0.2.2/src/d2l/client.py)
- [Course resolver](https://github.com/Aaryan-Kapoor/d2l-cli/blob/v0.2.2/src/d2l/resolver.py)
- [Authentication](https://github.com/Aaryan-Kapoor/d2l-cli/blob/v0.2.2/src/d2l/commands/auth_cmd.py)
- [Downloads](https://github.com/Aaryan-Kapoor/d2l-cli/blob/v0.2.2/src/d2l/commands/download.py)
- [SimpleSyllabus](https://github.com/Aaryan-Kapoor/d2l-cli/blob/v0.2.2/src/d2l/commands/syllabus.py)
- [Snapshot aggregation](https://github.com/Aaryan-Kapoor/d2l-cli/blob/v0.2.2/src/d2l/commands/dump.py)
