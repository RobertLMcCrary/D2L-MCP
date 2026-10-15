# MCP tool reference

- `d2l_status`: configuration, token, API, and next action
- `d2l_whoami`: current Brightspace identity
- `d2l_list_courses`: active or all enrollments
- `d2l_get_grades`: course grade values or final grade
- `d2l_list_assignments`: assignments and attachment metadata
- `d2l_list_quizzes`: quizzes and availability
- `d2l_get_discussions`: forums, topics, or posts
- `d2l_get_content`: course root, table of contents, or module structure
- `d2l_get_announcements`: course announcements
- `d2l_get_deadlines`: primary deadline view built from course assignments, quizzes and content, with status; calendar merged as a supplement
- `d2l_get_calendar`: global or course calendar events; incomplete on its own
- `d2l_get_due` / `d2l_get_overdue`: deadline views
- `d2l_get_updates`: unread/update counts
- `d2l_get_syllabus`: SimpleSyllabus text and metadata
- `d2l_get_snapshot`: comprehensive academic context
- `d2l_download_assignment`: assignment attachments
- `d2l_download_module`: recursively download module file topics
- `d2l_download_topic`: download one topic file

With `d2l-mcp setup --enable-writes`:

- `d2l_submit_assignment`: upload files to an assignment folder
- `d2l_post_discussion` / `d2l_edit_discussion_post` / `d2l_delete_discussion_post`: discussion posts
- `d2l_set_discussion_post_state`: read, flag, rating
- `d2l_update_content_progress`: mark a content topic read or completed
- `lms_api_write`: other documented `/d2l/api/` mutations

Run standalone setup and authentication with `d2l-mcp setup`, `d2l-mcp login`, and `d2l-mcp doctor`.
