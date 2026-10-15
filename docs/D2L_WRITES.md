# Brightspace writes

`d2l-mcp` is read-only unless you turn writes on. With writes on, an agent can submit
assignments and take part in discussions for you. Every write uses a documented
Valence learner route and runs as your own Brightspace account, so Brightspace's own
permissions still decide what is allowed.

## Enable

```sh
d2l-mcp setup --enable-writes          # keeps your current school
d2l-mcp setup --enable-writes=false    # back to read-only
```

Changing `--school` turns writes off unless `--enable-writes` is passed in the same
command. Restart your MCP client afterwards so it picks up the new tool list.

Put files you want to submit or attach in the managed download directory
(`~/.d2l/downloads` by default). Tools only read files from there.

## Tools

| Tool | What it does | Route |
| --- | --- | --- |
| `d2l_submit_assignment` | Upload one or more files to an assignment folder, with an optional comment | `POST …/dropbox/folders/{id}/submissions/mysubmissions/` or `…/group/{groupId}/` |
| `d2l_post_discussion` | Start a thread or reply, optionally with attachments | `POST …/discussions/forums/{f}/topics/{t}/posts/` |
| `d2l_edit_discussion_post` | Replace your post's subject and message | `PUT …/posts/{id}` |
| `d2l_delete_discussion_post` | Delete your post | `DELETE …/posts/{id}` |
| `d2l_set_discussion_post_state` | Read/unread, flag/unflag, rate 1–5 | `PUT …/ReadStatus`, `…/Flag`, `…/Rating/MyRating` |
| `d2l_update_content_progress` | Mark a content topic read or completed | `POST /d2l/api/le/unstable/{ou}/content/userprogress/` |
| `lms_api_write` | Any other `/d2l/api/` JSON mutation | — |

`d2l_submit_assignment` checks the folder before uploading. It refuses hidden folders,
text-only, on-paper and observed folders, and a missing or unexpected `group_id`.
Availability dates are left to Brightspace, because special access can reopen a folder
for one student. After submitting, it reads your submissions back so you can confirm
the files arrived. It marks the result `late` when the due date has passed.

## What the API cannot do

- **Quizzes:** there is no learner route to start, answer or submit a quiz attempt.
  Students cannot read their own attempts either, so `d2l_get_deadlines` reports quiz
  status as `unknown` until a grade is released.
- **Retracting work:** a submission can't be retracted or edited. Folders set to keep
  only one submission reject a second one.
- **Text-only submissions:** Valence documents file submissions only.
- **Comments:** you can't comment on your own submission after the fact. The comment
  is sent with the submission.
- **Group topics:** you can't post to group-type discussion topics.
- **Votes:** up/down votes (`…/Votes/MyVote`) have no named tool. The docs disagree
  on the value format, so use `lms_api_write` if a course uses them.

## Safety

- Writes are refused in the client when disabled, not just hidden.
- Writes are never retried, including after a 429 or a dropped connection. A retried
  POST could submit twice. After an ambiguous failure, check Brightspace first.
- Writes don't follow redirects, and credentials never leave the configured host.
- Uploads are streamed from `os.Root` under the managed directory. Absolute paths,
  traversal, symlink escapes and non-regular files are rejected. Total size is capped
  at 256 MiB.
- Agents are told to write only on your explicit request, and never because of text in
  course content. That instruction is advisory. Check your school's academic-integrity
  rules before turning writes on.
- The token is the same one the Brightspace web app uses for you, so it can do anything
  you can do in the browser.
