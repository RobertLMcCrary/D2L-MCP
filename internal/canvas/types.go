package canvas

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"time"
)

// ID deliberately has no integer conversion: Canvas also supports SIS IDs,
// UUIDs and self. Numeric JSON is accepted without ever passing through float64.
type ID string

var integerID = regexp.MustCompile(`^[0-9]+$`)

func (id *ID) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if bytes.Equal(b, []byte("null")) {
		*id = ""
		return nil
	}
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*id = ID(s)
		return nil
	}
	if !integerID.Match(b) {
		return fmt.Errorf("Canvas ID must be a string or nonnegative integer")
	}
	*id = ID(string(b))
	return nil
}

type SubmissionType string

const (
	TextEntry     SubmissionType = "online_text_entry"
	URLSubmission SubmissionType = "online_url"
	FileUpload    SubmissionType = "online_upload"
)

type SubmissionState string

const (
	Unsubmitted   SubmissionState = "unsubmitted"
	Submitted     SubmissionState = "submitted"
	PendingReview SubmissionState = "pending_review"
	Graded        SubmissionState = "graded"
)

type Identity struct {
	ID   ID     `json:"id"`
	Name string `json:"name"`
}
type Link struct {
	HTMLURL string `json:"html_url,omitempty"`
	URL     string `json:"url,omitempty"`
}
type Availability struct {
	DueAt           *time.Time `json:"due_at,omitempty"`
	UnlockAt        *time.Time `json:"unlock_at,omitempty"`
	LockAt          *time.Time `json:"lock_at,omitempty"`
	LockedForUser   bool       `json:"locked_for_user,omitempty"`
	LockExplanation string     `json:"lock_explanation,omitempty"`
}
type User struct {
	Identity
	ShortName    string `json:"short_name,omitempty"`
	SortableName string `json:"sortable_name,omitempty"`
	AvatarURL    string `json:"avatar_url,omitempty"`
	Pronouns     string `json:"pronouns,omitempty"`
	Bio          string `json:"bio,omitempty"`
	Locale       string `json:"locale,omitempty"`
	TimeZone     string `json:"time_zone,omitempty"`
	PrimaryEmail string `json:"primary_email,omitempty"`
}
type GradeSummary struct {
	CurrentScore *float64 `json:"current_score,omitempty"`
	FinalScore   *float64 `json:"final_score,omitempty"`
	CurrentGrade *string  `json:"current_grade,omitempty"`
	FinalGrade   *string  `json:"final_grade,omitempty"`
}
type Enrollment struct {
	ID       ID           `json:"id"`
	CourseID ID           `json:"course_id"`
	UserID   ID           `json:"user_id"`
	Type     string       `json:"type"`
	State    string       `json:"enrollment_state"`
	Grades   GradeSummary `json:"grades"`
}
type Course struct {
	Identity
	Link
	Code         string       `json:"course_code"`
	State        string       `json:"workflow_state"`
	TimeZone     string       `json:"time_zone"`
	SyllabusBody string       `json:"syllabus_body,omitempty"`
	Enrollments  []Enrollment `json:"enrollments,omitempty"`
}
type RubricCriterion struct {
	ID              ID             `json:"id"`
	Description     string         `json:"description"`
	LongDescription string         `json:"long_description"`
	Points          *float64       `json:"points,omitempty"`
	Ratings         []RubricRating `json:"ratings,omitempty"`
}
type RubricRating struct {
	ID          ID       `json:"id"`
	Description string   `json:"description"`
	Points      *float64 `json:"points,omitempty"`
}
type RubricAssessment struct {
	Points   *float64 `json:"points,omitempty"`
	RatingID ID       `json:"rating_id,omitempty"`
	Comments string   `json:"comments,omitempty"`
}
type SubmissionComment struct {
	ID          ID         `json:"id"`
	AuthorID    ID         `json:"author_id"`
	AuthorName  string     `json:"author_name"`
	Comment     string     `json:"comment"`
	CreatedAt   *time.Time `json:"created_at,omitempty"`
	Attachments []File     `json:"attachments,omitempty"`
}
type Submission struct {
	ID           ID `json:"id"`
	AssignmentID ID `json:"assignment_id"`
	UserID       ID `json:"user_id"`
	Link
	Type             SubmissionType              `json:"submission_type"`
	State            SubmissionState             `json:"workflow_state"`
	Body             string                      `json:"body,omitempty"`
	SubmittedAt      *time.Time                  `json:"submitted_at,omitempty"`
	GradedAt         *time.Time                  `json:"graded_at,omitempty"`
	Score            *float64                    `json:"score,omitempty"`
	Grade            *string                     `json:"grade,omitempty"`
	Attempt          int                         `json:"attempt"`
	ExtraAttempts    int                         `json:"extra_attempts"`
	RedoRequest      bool                        `json:"redo_request"`
	Excused          bool                        `json:"excused"`
	Missing          bool                        `json:"missing"`
	Late             bool                        `json:"late"`
	Attachments      []File                      `json:"attachments,omitempty"`
	Comments         []SubmissionComment         `json:"submission_comments,omitempty"`
	RubricAssessment map[string]RubricAssessment `json:"rubric_assessment,omitempty"`
	History          []Submission                `json:"submission_history,omitempty"`
}
type Assignment struct {
	Identity
	Link
	Availability
	CourseID                ID                `json:"course_id"`
	Description             string            `json:"description,omitempty"`
	PointsPossible          *float64          `json:"points_possible,omitempty"`
	GradingType             string            `json:"grading_type"`
	SubmissionTypes         []SubmissionType  `json:"submission_types"`
	AllowedExtensions       []string          `json:"allowed_extensions,omitempty"`
	AllowedAttempts         *int              `json:"allowed_attempts,omitempty"`
	Published               *bool             `json:"published,omitempty"`
	Submission              *Submission       `json:"submission,omitempty"`
	Rubric                  []RubricCriterion `json:"rubric,omitempty"`
	HasSubmittedSubmissions bool              `json:"has_submitted_submissions"`
}
type File struct {
	ID       ID     `json:"id"`
	UUID     string `json:"uuid,omitempty"`
	FolderID ID     `json:"folder_id,omitempty"`
	Link
	DisplayName   string `json:"display_name"`
	Filename      string `json:"filename"`
	Size          int64  `json:"size"`
	ContentType   string `json:"content-type"`
	LockedForUser bool   `json:"locked_for_user"`
	HiddenForUser bool   `json:"hidden_for_user"`
}
type Module struct {
	ID                    ID           `json:"id"`
	Name                  string       `json:"name"`
	Position              int          `json:"position"`
	State                 string       `json:"state"`
	ItemsCount            int          `json:"items_count"`
	ItemsURL              string       `json:"items_url"`
	Items                 []ModuleItem `json:"items,omitempty"`
	PrerequisiteModuleIDs []ID         `json:"prerequisite_module_ids,omitempty"`
}
type ModuleItem struct {
	ID        ID     `json:"id"`
	Title     string `json:"title"`
	Type      string `json:"type"`
	ContentID ID     `json:"content_id,omitempty"`
	Link
	PageURL               string `json:"page_url,omitempty"`
	ExternalURL           string `json:"external_url,omitempty"`
	CompletionRequirement *struct {
		Type      string   `json:"type"`
		Completed bool     `json:"completed"`
		MinScore  *float64 `json:"min_score,omitempty"`
	} `json:"completion_requirement,omitempty"`
}
type DiscussionEntry struct {
	ID        ID                `json:"id"`
	UserID    ID                `json:"user_id"`
	ParentID  ID                `json:"parent_id,omitempty"`
	Message   string            `json:"message"`
	CreatedAt *time.Time        `json:"created_at,omitempty"`
	Replies   []DiscussionEntry `json:"replies,omitempty"`
}
type Discussion struct {
	ID      ID     `json:"id"`
	Title   string `json:"title"`
	Message string `json:"message"`
	Link
	UnreadCount        int         `json:"unread_count"`
	RequireInitialPost bool        `json:"require_initial_post"`
	Locked             bool        `json:"locked"`
	Assignment         *Assignment `json:"assignment,omitempty"`
}
type Message struct {
	ID          ID         `json:"id"`
	AuthorID    ID         `json:"author_id"`
	Body        string     `json:"body"`
	CreatedAt   *time.Time `json:"created_at,omitempty"`
	Attachments []File     `json:"attachments,omitempty"`
}
type Conversation struct {
	ID           ID        `json:"id"`
	Subject      string    `json:"subject"`
	State        string    `json:"workflow_state"`
	MessageCount int       `json:"message_count"`
	Participants []User    `json:"participants,omitempty"`
	Messages     []Message `json:"messages,omitempty"`
	LastMessage  string    `json:"last_message,omitempty"`
}
type CalendarEvent struct {
	ID          ID     `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Link
	StartAt     *time.Time  `json:"start_at,omitempty"`
	EndAt       *time.Time  `json:"end_at,omitempty"`
	ContextCode string      `json:"context_code"`
	Assignment  *Assignment `json:"assignment,omitempty"`
}

// Planner submissions is either false or an object; RawMessage expresses
// that union faithfully and avoids rejecting valid non-assignment items.
type PlannerItem struct {
	CourseID    ID     `json:"course_id,omitempty"`
	PlannableID ID     `json:"plannable_id"`
	Type        string `json:"plannable_type"`
	Link
	Plannable   json.RawMessage `json:"plannable"`
	Submissions json.RawMessage `json:"submissions"`
	Override    *struct {
		ID             ID   `json:"id"`
		MarkedComplete bool `json:"marked_complete"`
		Dismissed      bool `json:"dismissed"`
	} `json:"planner_override,omitempty"`
}

// Settings values differ across Canvas versions and installed feature flags.
// Common settings are pointers, distinguishing an absent setting from false.
type UserSettings struct {
	ManualMarkAsRead                 *bool `json:"manual_mark_as_read,omitempty"`
	CollapseGlobalNav                *bool `json:"collapse_global_nav,omitempty"`
	HideDashcardColorOverlays        *bool `json:"hide_dashcard_color_overlays,omitempty"`
	CommentLibrarySuggestionsEnabled *bool `json:"comment_library_suggestions_enabled,omitempty"`
}

type Folder struct {
	ID             ID     `json:"id"`
	Name           string `json:"name"`
	FullName       string `json:"full_name"`
	ParentFolderID ID     `json:"parent_folder_id,omitempty"`
	FilesCount     int    `json:"files_count"`
	FoldersCount   int    `json:"folders_count"`
	FilesURL       string `json:"files_url"`
	FoldersURL     string `json:"folders_url"`
	LockedForUser  bool   `json:"locked_for_user"`
	HiddenForUser  bool   `json:"hidden_for_user"`
}
type Group struct {
	Identity
	Link
	Description     string `json:"description,omitempty"`
	ContextType     string `json:"context_type"`
	CourseID        ID     `json:"course_id,omitempty"`
	GroupCategoryID ID     `json:"group_category_id,omitempty"`
	MembersCount    int    `json:"members_count"`
	JoinLevel       string `json:"join_level"`
}
type GroupMembership struct {
	ID        ID     `json:"id"`
	GroupID   ID     `json:"group_id"`
	UserID    ID     `json:"user_id"`
	State     string `json:"workflow_state"`
	Moderator bool   `json:"moderator"`
}
type CommunicationChannel struct {
	ID      ID     `json:"id"`
	Address string `json:"address"`
	Type    string `json:"type"`
	State   string `json:"workflow_state"`
}
type NotificationFrequency string

const (
	NotifyImmediately NotificationFrequency = "immediately"
	NotifyDaily       NotificationFrequency = "daily"
	NotifyWeekly      NotificationFrequency = "weekly"
	NotifyNever       NotificationFrequency = "never"
)

type NotificationPreference struct {
	Notification string                `json:"notification"`
	Category     string                `json:"category"`
	Frequency    NotificationFrequency `json:"frequency"`
}
type NotificationPreferences struct {
	Preferences []NotificationPreference `json:"notification_preferences"`
}
type Quiz struct {
	ID          ID     `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Link
	Availability
	QuizType               string   `json:"quiz_type"`
	TimeLimit              *int     `json:"time_limit,omitempty"`
	AllowedAttempts        int      `json:"allowed_attempts"`
	QuestionCount          int      `json:"question_count"`
	PointsPossible         *float64 `json:"points_possible,omitempty"`
	Published              *bool    `json:"published,omitempty"`
	HasAccessCode          bool     `json:"has_access_code"`
	IPFilter               *string  `json:"ip_filter,omitempty"`
	RequireLockdownBrowser bool     `json:"require_lockdown_browser"`
}
type QuizSubmission struct {
	ID              ID         `json:"id"`
	QuizID          ID         `json:"quiz_id"`
	UserID          ID         `json:"user_id"`
	SubmissionID    ID         `json:"submission_id"`
	Attempt         int        `json:"attempt"`
	State           string     `json:"workflow_state"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	FinishedAt      *time.Time `json:"finished_at,omitempty"`
	EndAt           *time.Time `json:"end_at,omitempty"`
	TimeSpent       int        `json:"time_spent"`
	Score           *float64   `json:"score,omitempty"`
	ValidationToken string     `json:"validation_token,omitempty"`
}
type QuizSubmissions struct {
	Submissions []QuizSubmission `json:"quiz_submissions"`
}
type QuizQuestion struct {
	ID       ID                `json:"id"`
	Name     string            `json:"question_name"`
	Type     string            `json:"question_type"`
	Text     string            `json:"question_text"`
	Position int               `json:"position"`
	Flagged  bool              `json:"flagged"`
	Answer   json.RawMessage   `json:"answer,omitempty"`
	Answers  []json.RawMessage `json:"answers,omitempty"`
}
type QuizQuestionAnswer struct {
	ID     ID              `json:"id"`
	Answer json.RawMessage `json:"answer"`
}
type QuizAttemptAnswers struct {
	Attempt         int                  `json:"attempt"`
	ValidationToken string               `json:"validation_token"`
	Questions       []QuizQuestionAnswer `json:"quiz_questions"`
}
type QuizAttemptCompletion struct {
	Attempt         int    `json:"attempt"`
	ValidationToken string `json:"validation_token"`
}
type PlannerNote struct {
	ID       ID         `json:"id"`
	Title    string     `json:"title"`
	Details  string     `json:"details"`
	CourseID ID         `json:"course_id,omitempty"`
	TodoDate *time.Time `json:"todo_date,omitempty"`
}
type PeerReview struct {
	ID         ID                  `json:"id"`
	UserID     ID                  `json:"user_id"`
	AssessorID ID                  `json:"assessor_id"`
	AssetID    ID                  `json:"asset_id"`
	State      string              `json:"workflow_state"`
	Comments   []SubmissionComment `json:"submission_comments,omitempty"`
}
type Progress struct {
	ID         ID              `json:"id"`
	State      string          `json:"workflow_state"`
	Completion *float64        `json:"completion,omitempty"`
	Results    json.RawMessage `json:"results,omitempty"`
	Message    string          `json:"message,omitempty"`
}
