package canvas

import (
	"context"
	"net/url"
	"regexp"
	"strings"

	"github.com/RobertLMcCrary/D2L-MCP/internal/lms"
)

// Operation is a declarative recipe, shared by tools, documentation and tests.
// Query parameters remain open so Canvas extensions do not require a release.
type Operation struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Path        string     `json:"path"`
	List        bool       `json:"list"`
	Defaults    url.Values `json:"defaults,omitempty"`
}

var Operations = []Operation{
	{"whoami", "Authenticated user profile", "/api/v1/users/self/profile", false, nil},
	{"list_courses", "Enrolled courses including grade summaries; enrollment_state can select past courses", "/api/v1/courses", true, url.Values{"include[]": {"total_scores"}}},
	{"get_course", "Course details and syllabus HTML", "/api/v1/courses/{course_id}", false, url.Values{"include[]": {"syllabus_body", "total_scores"}}},
	{"list_assignments", "Assignments with the current student's submission, individualized due dates and allowed submission types", "/api/v1/courses/{course_id}/assignments", true, url.Values{"include[]": {"submission"}}},
	{"get_assignment", "Assignment instructions, rubric, restrictions and own submission", "/api/v1/courses/{course_id}/assignments/{assignment_id}", false, url.Values{"include[]": {"submission"}}},
	{"get_submission", "Own submission, grade, comments, rubric feedback and submission history", "/api/v1/courses/{course_id}/assignments/{assignment_id}/submissions/self", false, url.Values{"include[]": {"submission_comments", "rubric_assessment", "submission_history"}}},
	{"list_assignment_groups", "Assignment groups and grading weights", "/api/v1/courses/{course_id}/assignment_groups", true, url.Values{"include[]": {"assignments"}}},
	{"get_grades", "Own enrollment and current/final grade summaries; hidden grades stay hidden", "/api/v1/courses/{course_id}/enrollments", true, url.Values{"user_id": {"self"}}},
	{"list_modules", "Modules and prerequisites; list_module_items retrieves complete item lists even when inline items are truncated", "/api/v1/courses/{course_id}/modules", true, nil},
	{"list_module_items", "Complete paginated module items including requirements and content links", "/api/v1/courses/{course_id}/modules/{module_id}/items", true, nil},
	{"get_module_item", "Single module item and completion requirement", "/api/v1/courses/{course_id}/modules/{module_id}/items/{item_id}", false, nil},
	{"list_pages", "Published course page metadata", "/api/v1/courses/{course_id}/pages", true, nil},
	{"get_page", "Course page HTML by page slug or page ID", "/api/v1/courses/{course_id}/pages/{page}", false, nil},
	{"get_front_page", "Course front page HTML", "/api/v1/courses/{course_id}/front_page", false, nil},
	{"list_files", "Accessible course files and download metadata", "/api/v1/courses/{course_id}/files", true, nil},
	{"get_file", "File metadata, lock information and signed download URL", "/api/v1/files/{file_id}", false, nil},
	{"list_folders", "Course folder hierarchy", "/api/v1/courses/{course_id}/folders", true, nil},
	{"list_folder_files", "Files in a folder", "/api/v1/folders/{folder_id}/files", true, nil},
	{"list_announcements", "Course announcements including announcement discussion metadata", "/api/v1/courses/{course_id}/discussion_topics", true, url.Values{"only_announcements": {"true"}}},
	{"list_discussions", "Course discussions, unread counts and reply restrictions", "/api/v1/courses/{course_id}/discussion_topics", true, nil},
	{"get_discussion", "Discussion details and initial message", "/api/v1/courses/{course_id}/discussion_topics/{topic_id}", false, nil},
	{"get_discussion_view", "Cached discussion view plus new_entries from include_new_entries; inspect both because the cached view is eventually consistent", "/api/v1/courses/{course_id}/discussion_topics/{topic_id}/view", false, url.Values{"include_new_entries": {"true"}}},
	{"list_discussion_entries", "Paginated top-level discussion entries; use list_discussion_replies for complete replies", "/api/v1/courses/{course_id}/discussion_topics/{topic_id}/entries", true, nil},
	{"list_discussion_replies", "Complete paginated replies to an entry", "/api/v1/courses/{course_id}/discussion_topics/{topic_id}/entries/{entry_id}/replies", true, nil},
	{"list_conversations", "Inbox conversations; scope can select unread, sent, or archived", "/api/v1/conversations", true, nil},
	{"get_conversation", "Inbox thread and messages without marking it read", "/api/v1/conversations/{conversation_id}", false, url.Values{"auto_mark_as_read": {"false"}}},
	{"get_calendar", "Calendar events; use context_codes[], start_date, end_date and type=assignment for assignment events", "/api/v1/calendar_events", true, nil},
	{"get_planner", "Student planner items with submission and override status", "/api/v1/planner/items", true, nil},
	{"list_planner_notes", "Personal planner reminders", "/api/v1/planner_notes", true, nil},
	{"get_todo", "User todo list as provided by Canvas; not an exhaustive deadline history", "/api/v1/users/self/todo", true, nil},
	{"get_upcoming_events", "Upcoming user events", "/api/v1/users/self/upcoming_events", true, nil},
	{"get_missing_assignments", "Canvas-marked missing assignments for the authenticated student", "/api/v1/users/self/missing_submissions", true, nil},
	{"list_quizzes", "Classic Quiz metadata; attempting quizzes requires the quiz workflow and school permissions", "/api/v1/courses/{course_id}/quizzes", true, nil},
	{"get_quiz", "Classic Quiz details", "/api/v1/courses/{course_id}/quizzes/{quiz_id}", false, nil},
	{"list_quiz_submissions", "Classic Quiz submission envelope; quiz_submissions array is preserved", "/api/v1/courses/{course_id}/quizzes/{quiz_id}/submissions", false, nil},
	{"list_new_quizzes", "New Quizzes metadata, using its separate API namespace; school permissions may restrict access", "/api/quiz/v1/courses/{course_id}/quizzes", true, nil},
	{"list_groups", "Current user's groups", "/api/v1/users/self/groups", true, nil},
	{"get_group", "Study or assignment group details", "/api/v1/groups/{group_id}", false, nil},
	{"list_group_discussions", "Group discussion topics", "/api/v1/groups/{group_id}/discussion_topics", true, nil},
	{"get_tabs", "Visible course navigation tabs and external-tool links", "/api/v1/courses/{course_id}/tabs", true, nil},
	{"list_rubrics", "Course rubrics; permissions may restrict this collection", "/api/v1/courses/{course_id}/rubrics", true, nil},
	{"get_notifications", "User communication channels", "/api/v1/users/self/communication_channels", true, nil},
	{"get_user", "Own user details and permission hints", "/api/v1/users/self", false, nil},
	{"get_settings", "Own Canvas settings including read-state and dashboard preferences", "/api/v1/users/self/settings", false, nil},
	{"get_colors", "Own dashboard course and group colors", "/api/v1/users/self/colors", false, nil},
	{"get_dashboard_positions", "Own dashboard card ordering", "/api/v1/users/self/dashboard_positions", false, nil},
	{"list_course_nicknames", "Own course nicknames", "/api/v1/users/self/course_nicknames", true, nil},
	{"list_favorite_courses", "Favorite courses or Canvas default selection", "/api/v1/users/self/favorites/courses", true, nil},
	{"list_favorite_groups", "Favorite groups or Canvas default selection", "/api/v1/users/self/favorites/groups", true, nil},
	{"list_avatars", "Available avatar choices and opaque avatar tokens", "/api/v1/users/self/avatars", true, nil},
	{"get_activity", "Own recent activity stream", "/api/v1/users/self/activity_stream", true, nil},
	{"get_activity_summary", "Own activity stream summary", "/api/v1/users/self/activity_stream/summary", true, nil},
	{"get_todo_count", "Todo counts", "/api/v1/users/self/todo_item_count", false, nil},
	{"get_recent_feedback", "Recently graded submissions across courses", "/api/v1/users/self/graded_submissions", true, nil},
	{"get_notification_preferences", "Notification preferences envelope for a communication channel type and address", "/api/v1/users/self/communication_channels/{type}/{address}/notification_preferences", false, nil},
	{"get_notification_categories", "Notification category envelope for a channel", "/api/v1/users/self/communication_channels/{channel_id}/notification_preference_categories", false, nil},
	{"get_notification_preference", "Single notification preference envelope", "/api/v1/users/self/communication_channels/{channel_id}/notification_preferences/{notification}", false, nil},
	{"list_user_files", "Personal files", "/api/v1/users/self/files", true, nil},
	{"list_user_folders", "Personal folder hierarchy", "/api/v1/users/self/folders", true, nil},
	{"list_group_files", "Files in a study or assignment group", "/api/v1/groups/{group_id}/files", true, nil},
	{"list_group_pages", "Group wiki pages", "/api/v1/groups/{group_id}/pages", true, nil},
	{"get_group_page", "Group wiki page HTML", "/api/v1/groups/{group_id}/pages/{page}", false, nil},
	{"list_group_members", "Members of a group; permissions apply", "/api/v1/groups/{group_id}/users", true, nil},
	{"list_peer_reviews", "Own assigned peer reviews; Canvas role governs visibility", "/api/v1/courses/{course_id}/assignments/{assignment_id}/peer_reviews", true, url.Values{"include[]": {"submission_comments", "user"}}},
	{"list_collaborations", "Course collaborations and external editing links", "/api/v1/courses/{course_id}/collaborations", true, nil},
	{"get_conferences", "Course web conferences envelope", "/api/v1/courses/{course_id}/conferences", false, nil},
	{"list_appointment_groups", "Available appointment groups; query scope and context_codes[]", "/api/v1/appointment_groups", true, nil},
	{"get_appointment_group", "Appointment group and slots; include[]=appointments requests slots", "/api/v1/appointment_groups/{appointment_group_id}", false, url.Values{"include[]": {"appointments"}}},
	{"get_calendar_event", "Single calendar event or appointment slot", "/api/v1/calendar_events/{event_id}", false, nil},
	{"get_progress", "Poll a Canvas asynchronous operation", "/api/v1/progress/{progress_id}", false, nil},
	{"get_course_permissions", "Check requested permissions[] for the current user", "/api/v1/courses/{course_id}/permissions", false, nil},
	{"get_group_permissions", "Check requested permissions[] for the current group member", "/api/v1/groups/{group_id}/permissions", false, nil},
	{"get_course_progress", "Own module completion progress", "/api/v1/courses/{course_id}/users/self/progress", false, nil},
	{"get_module", "Module state, prerequisites and completion details", "/api/v1/courses/{course_id}/modules/{module_id}", false, nil},
	{"get_module_sequence", "Previous and next module items; query asset_type/asset_id or asset_string", "/api/v1/courses/{course_id}/module_item_sequence", false, nil},
	{"list_course_people", "Visible course people; query enrollment_type or search_term; school privacy rules apply", "/api/v1/courses/{course_id}/users", true, nil},
	{"get_course_activity", "Course activity stream", "/api/v1/courses/{course_id}/activity_stream", true, nil},
	{"get_group_activity", "Group activity stream", "/api/v1/groups/{group_id}/activity_stream", true, nil},
	{"list_course_groups", "Visible course groups", "/api/v1/courses/{course_id}/groups", true, nil},
	{"list_group_memberships", "Group memberships and membership state", "/api/v1/groups/{group_id}/memberships", true, nil},
	{"get_own_group_membership", "Own group membership", "/api/v1/groups/{group_id}/users/self", false, nil},
	{"list_group_folders", "Group folder hierarchy", "/api/v1/groups/{group_id}/folders", true, nil},
	{"list_subfolders", "Child folders", "/api/v1/folders/{folder_id}/folders", true, nil},
	{"get_folder", "Folder metadata and access restrictions", "/api/v1/folders/{folder_id}", false, nil},
	{"get_user_file_quota", "Personal storage quota and usage", "/api/v1/users/self/files/quota", false, nil},
	{"get_group_file_quota", "Group storage quota and usage", "/api/v1/groups/{group_id}/files/quota", false, nil},
	{"get_planner_note", "One planner reminder", "/api/v1/planner_notes/{note_id}", false, nil},
	{"list_planner_overrides", "Planner completion and dismissal overrides", "/api/v1/planner/overrides", true, nil},
	{"get_planner_override", "One planner override", "/api/v1/planner/overrides/{override_id}", false, nil},
	{"get_own_quiz_submission", "Own Classic Quiz attempt envelope", "/api/v1/courses/{course_id}/quizzes/{quiz_id}/submission", false, nil},
	{"get_quiz_submission", "One permitted Classic Quiz attempt envelope", "/api/v1/courses/{course_id}/quizzes/{quiz_id}/submissions/{submission_id}", false, nil},
	{"get_quiz_submission_time", "Remaining time for a Classic Quiz attempt", "/api/v1/courses/{course_id}/quizzes/{quiz_id}/submissions/{submission_id}/time", false, nil},
	{"get_quiz_submission_questions", "Classic Quiz attempt question envelope; access code, attempt and validation token may be required", "/api/v1/quiz_submissions/{submission_id}/questions", false, nil},
	{"get_quiz_formatted_answer", "Formatted answer to an attempt question", "/api/v1/quiz_submissions/{submission_id}/questions/{question_id}/formatted_answer", false, nil},
	{"list_eportfolios", "Own ePortfolio metadata; editing pages generally requires the browser", "/api/v1/users/self/eportfolios", true, nil},
	{"get_eportfolio", "Accessible ePortfolio metadata", "/api/v1/eportfolios/{portfolio_id}", false, nil},
	{"list_eportfolio_pages", "Accessible ePortfolio page metadata", "/api/v1/eportfolios/{portfolio_id}/pages", true, nil},
	{"get_outcome_results", "Learning outcome result envelope; query user_ids[] to scope to own user ID", "/api/v1/courses/{course_id}/outcome_results", false, nil},
	{"get_outcome_rollups", "Learning mastery rollup envelope; query user_ids[] for own user ID", "/api/v1/courses/{course_id}/outcome_rollups", false, nil},
}

var placeholder = regexp.MustCompile(`\{([a-z_]+)\}`)

var EnvelopeCollections = map[string]string{
	"list_quiz_submissions":         "quiz_submissions",
	"get_quiz_submission_questions": "quiz_submission_questions",
	"get_conferences":               "conferences",
	"get_outcome_results":           "outcome_results",
	"get_outcome_rollups":           "rollups",
}

func (op Operation) Request(parameters map[string]string, query url.Values) (lms.Request, error) {
	var missing string
	p := placeholder.ReplaceAllStringFunc(op.Path, func(part string) string {
		key := strings.Trim(part, "{}")
		value := parameters[key]
		if value == "" || strings.ContainsAny(value, "/\\%?#\x00\r\n") || value == "." || value == ".." {
			missing = key
			return part
		}
		return url.PathEscape(value)
	})
	if missing != "" {
		return lms.Request{}, lms.Invalid("A safe path parameter is required: " + missing)
	}
	q := url.Values{}
	for key, values := range op.Defaults {
		q[key] = append([]string(nil), values...)
	}
	for key, values := range query {
		q[key] = append([]string(nil), values...)
	}
	// This named read tool must never mutate inbox read state.
	if op.Name == "get_conversation" {
		q.Set("auto_mark_as_read", "false")
	}
	collection := EnvelopeCollections[op.Name]
	return lms.Request{Path: p, Query: q, Paginate: op.List || collection != "", Collection: collection}, nil
}

func (c *Client) Read(ctx context.Context, name string, parameters map[string]string, query url.Values) (lms.Response, error) {
	for _, op := range Operations {
		if op.Name == name {
			r, err := op.Request(parameters, query)
			if err != nil {
				return lms.Response{}, err
			}
			return c.Do(ctx, r)
		}
	}
	return lms.Response{}, lms.Invalid("Unknown Canvas operation")
}

// ReadAs composes a catalog recipe with any typed projection while preserving
// the full response in Document.Raw. Go methods cannot have type parameters.
func ReadAs[T any](ctx context.Context, client *Client, name string, parameters map[string]string, query url.Values) (lms.TypedResponse[T], error) {
	for _, op := range Operations {
		if op.Name == name {
			request, err := op.Request(parameters, query)
			if err != nil {
				return lms.TypedResponse[T]{}, err
			}
			return lms.Fetch[T](ctx, client, request)
		}
	}
	return lms.TypedResponse[T]{}, lms.Invalid("Unknown Canvas operation")
}
