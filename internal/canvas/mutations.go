package canvas

import (
	"context"
	"encoding/json"
	"github.com/RobertLMcCrary/D2L-MCP/internal/lms"
)

// Mutation reuses read path composition. Typed bodies and extension bodies
// both use the same transport, write gate, and error handling.
type Mutation struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Method      string `json:"method"`
	Path        string `json:"path"`
	Destructive bool   `json:"destructive"`
}

var Mutations = []Mutation{
	{"update_settings", "Change own settings; body contains manual_mark_as_read, collapse_global_nav or hide_dashcard_color_overlays", "PUT", "/api/v1/users/self/settings", false},
	{"update_profile", "Edit own profile; body: {user: {name, short_name, bio, pronouns, locale, time_zone, avatar: {token}}}; school restrictions apply", "PUT", "/api/v1/users/self", false},
	{"set_color", "Set context color; body: {hexcode: '#aabbcc'}; asset_string is course_ID or group_ID", "PUT", "/api/v1/users/self/colors/{asset_string}", false},
	{"set_dashboard_positions", "Set dashboard order; body: {dashboard_positions: {course_ID: position}}", "PUT", "/api/v1/users/self/dashboard_positions", false},
	{"set_course_nickname", "Set own course nickname; body: {nickname: '...'}", "PUT", "/api/v1/users/self/course_nicknames/{course_id}", false},
	{"remove_course_nickname", "Remove own course nickname", "DELETE", "/api/v1/users/self/course_nicknames/{course_id}", true},
	{"favorite_course", "Add a course to favorites", "POST", "/api/v1/users/self/favorites/courses/{course_id}", false},
	{"unfavorite_course", "Remove a course from favorites", "DELETE", "/api/v1/users/self/favorites/courses/{course_id}", true},
	{"favorite_group", "Add a group to favorites", "POST", "/api/v1/users/self/favorites/groups/{group_id}", false},
	{"unfavorite_group", "Remove a group from favorites", "DELETE", "/api/v1/users/self/favorites/groups/{group_id}", true},
	{"set_notification_preference", "Set notification frequency; body: {notification_preferences: {frequency: immediately|daily|weekly|never}}", "PUT", "/api/v1/users/self/communication_channels/{channel_id}/notification_preferences/{notification}", false},
	{"set_editor_preference", "Set preferred editor; body: {text_editor_preference: rce|block_editor|''}; empty string restores the default", "PUT", "/api/v1/users/self/text_editor_preference", false},
	{"create_planner_note", "Create reminder; body contains title, details, todo_date and optional course_id", "POST", "/api/v1/planner_notes", false},
	{"update_planner_note", "Edit reminder; body contains title, details or todo_date", "PUT", "/api/v1/planner_notes/{note_id}", false},
	{"delete_planner_note", "Delete reminder", "DELETE", "/api/v1/planner_notes/{note_id}", true},
	{"create_planner_override", "Mark planner object completed/dismissed; body contains plannable_type, plannable_id, marked_complete or dismissed", "POST", "/api/v1/planner/overrides", false},
	{"update_planner_override", "Change marked_complete or dismissed", "PUT", "/api/v1/planner/overrides/{override_id}", false},
	{"delete_planner_override", "Delete planner override", "DELETE", "/api/v1/planner/overrides/{override_id}", true},
	{"send_message", "Send inbox message; body contains recipients (string IDs), subject, body and optional context_code", "POST", "/api/v1/conversations", false},
	{"reply_message", "Reply to inbox thread; body contains body and optional recipients or attachment_ids", "POST", "/api/v1/conversations/{conversation_id}/add_message", false},
	{"update_conversation", "Archive, star or change read state; body: {conversation: {workflow_state, subscribed, starred}}", "PUT", "/api/v1/conversations/{conversation_id}", false},
	{"delete_conversation", "Delete a conversation for the current user", "DELETE", "/api/v1/conversations/{conversation_id}", true},
	{"post_discussion_entry", "Post top-level reply; body contains message", "POST", "/api/v1/courses/{course_id}/discussion_topics/{topic_id}/entries", false},
	{"reply_discussion_entry", "Reply to discussion entry; body contains message", "POST", "/api/v1/courses/{course_id}/discussion_topics/{topic_id}/entries/{entry_id}/replies", false},
	{"edit_discussion_entry", "Edit own entry; body contains message", "PUT", "/api/v1/courses/{course_id}/discussion_topics/{topic_id}/entries/{entry_id}", false},
	{"delete_discussion_entry", "Delete own discussion entry", "DELETE", "/api/v1/courses/{course_id}/discussion_topics/{topic_id}/entries/{entry_id}", true},
	{"mark_discussion_read", "Mark a discussion topic read", "PUT", "/api/v1/courses/{course_id}/discussion_topics/{topic_id}/read", false},
	{"mark_discussion_unread", "Mark a discussion topic unread", "DELETE", "/api/v1/courses/{course_id}/discussion_topics/{topic_id}/read", false},
	{"subscribe_discussion", "Subscribe to discussion", "PUT", "/api/v1/courses/{course_id}/discussion_topics/{topic_id}/subscribed", false},
	{"unsubscribe_discussion", "Unsubscribe from discussion", "DELETE", "/api/v1/courses/{course_id}/discussion_topics/{topic_id}/subscribed", false},
	{"comment_submission", "Add own submission comment; body: {comment: {text_comment, file_ids}}", "PUT", "/api/v1/courses/{course_id}/assignments/{assignment_id}/submissions/self", false},
	{"complete_module_item", "Mark eligible module item complete", "PUT", "/api/v1/courses/{course_id}/modules/{module_id}/items/{item_id}/done", false},
	{"uncomplete_module_item", "Mark eligible module item incomplete", "DELETE", "/api/v1/courses/{course_id}/modules/{module_id}/items/{item_id}/done", false},
	{"reserve_appointment", "Reserve appointment for participant_id (self or group); optional body contains comments", "POST", "/api/v1/calendar_events/{event_id}/reservations/{participant_id}", false},
	{"create_calendar_event", "Create permitted personal event; body: {calendar_event: {context_code, title, start_at, end_at, description}}", "POST", "/api/v1/calendar_events", false},
	{"update_calendar_event", "Edit permitted event; body contains calendar_event fields", "PUT", "/api/v1/calendar_events/{event_id}", false},
	{"delete_calendar_event", "Delete event or cancel a permitted reservation", "DELETE", "/api/v1/calendar_events/{event_id}", true},
	{"mark_module_item_read", "Record a selected module item as read", "POST", "/api/v1/courses/{course_id}/modules/{module_id}/items/{item_id}/mark_read", false},
	{"select_mastery_path", "Choose an allowed mastery path; body contains assignment_set_id", "POST", "/api/v1/courses/{course_id}/modules/{module_id}/items/{item_id}/select_mastery_path", false},
	{"join_group", "Request own membership in an eligible self-signup group; body: {user_id: self}", "POST", "/api/v1/groups/{group_id}/memberships", false},
	{"leave_group", "Leave a group when self-signup policy allows it", "DELETE", "/api/v1/groups/{group_id}/users/self", true},
	{"update_file", "Rename or move a file the student controls; body contains name or parent_folder_id", "PUT", "/api/v1/files/{file_id}", false},
	{"delete_file", "Delete a file the student controls; submission attachment access can be affected", "DELETE", "/api/v1/files/{file_id}", true},
	{"create_user_folder", "Create personal folder; body contains name and optional parent_folder_id", "POST", "/api/v1/users/self/folders", false},
	{"create_group_folder", "Create permitted group folder; body contains name and optional parent_folder_id", "POST", "/api/v1/groups/{group_id}/folders", false},
	{"update_folder", "Rename or move a permitted folder; body contains name or parent_folder_id", "PUT", "/api/v1/folders/{folder_id}", false},
	{"delete_folder", "Delete a permitted folder; avoid force=true unless the user explicitly requested recursive removal", "DELETE", "/api/v1/folders/{folder_id}", true},
	{"create_group_page", "Create group wiki page; body: {wiki_page: {title, body}}", "POST", "/api/v1/groups/{group_id}/pages", false},
	{"update_group_page", "Edit permitted group wiki page; body: {wiki_page: {title, body}}", "PUT", "/api/v1/groups/{group_id}/pages/{page}", false},
	{"delete_group_page", "Delete a permitted group wiki page", "DELETE", "/api/v1/groups/{group_id}/pages/{page}", true},
	{"post_group_discussion_entry", "Post to a group discussion; body contains message", "POST", "/api/v1/groups/{group_id}/discussion_topics/{topic_id}/entries", false},
	{"reply_group_discussion_entry", "Reply to a group discussion entry; body contains message", "POST", "/api/v1/groups/{group_id}/discussion_topics/{topic_id}/entries/{entry_id}/replies", false},
	{"start_quiz_attempt", "Start an explicitly user-requested Classic Quiz attempt; body may need access_code. Honor course rules and time limits", "POST", "/api/v1/courses/{course_id}/quizzes/{quiz_id}/submissions", false},
	{"answer_quiz_questions", "Save user-provided Classic Quiz answers; body contains attempt, validation_token and quiz_questions array. Honor course rules", "POST", "/api/v1/quiz_submissions/{submission_id}/questions", false},
	{"complete_quiz_attempt", "Complete an explicitly user-requested Classic Quiz attempt; body contains attempt and validation_token", "POST", "/api/v1/courses/{course_id}/quizzes/{quiz_id}/submissions/{submission_id}/complete", false},
	{"flag_quiz_question", "Flag an attempt question; body contains attempt and validation_token", "PUT", "/api/v1/quiz_submissions/{submission_id}/questions/{question_id}/flag", false},
	{"unflag_quiz_question", "Unflag an attempt question; body contains attempt and validation_token", "PUT", "/api/v1/quiz_submissions/{submission_id}/questions/{question_id}/unflag", false},
}

func (c *Client) Mutate(ctx context.Context, op Mutation, parameters map[string]string, body any) (lms.Response, error) {
	r, err := (Operation{Path: op.Path}).Request(parameters, nil)
	if err != nil {
		return lms.Response{}, err
	}
	r.Method = op.Method
	if body != nil {
		r.Body, err = json.Marshal(body)
		if err != nil {
			return lms.Response{}, err
		}
	}
	return c.Do(ctx, r)
}

// Pointers preserve PATCH-like update semantics: absent differs from false or
// an explicitly empty string. These builders compose with Mutate or Do.
type ProfileUpdate struct {
	User struct {
		Name      *string `json:"name,omitempty"`
		ShortName *string `json:"short_name,omitempty"`
		Bio       *string `json:"bio,omitempty"`
		Pronouns  *string `json:"pronouns,omitempty"`
		Locale    *string `json:"locale,omitempty"`
		TimeZone  *string `json:"time_zone,omitempty"`
		Avatar    *struct {
			Token string `json:"token"`
		} `json:"avatar,omitempty"`
	} `json:"user"`
}
type InboxMessage struct {
	Recipients        []ID   `json:"recipients"`
	Subject           string `json:"subject"`
	Body              string `json:"body"`
	ContextCode       string `json:"context_code,omitempty"`
	AttachmentIDs     []ID   `json:"attachment_ids,omitempty"`
	GroupConversation *bool  `json:"group_conversation,omitempty"`
}
type DiscussionReply struct {
	Message string `json:"message"`
}

type InboxReply struct {
	Body          string `json:"body"`
	Recipients    []ID   `json:"recipients,omitempty"`
	AttachmentIDs []ID   `json:"attachment_ids,omitempty"`
}
type NotificationUpdate struct {
	Preferences struct {
		Frequency NotificationFrequency `json:"frequency"`
	} `json:"notification_preferences"`
}
type PlannerNoteUpdate struct {
	Title    *string `json:"title,omitempty"`
	Details  *string `json:"details,omitempty"`
	TodoDate *string `json:"todo_date,omitempty"`
	CourseID *ID     `json:"course_id,omitempty"`
}
