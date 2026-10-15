package d2l

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/RobertLMcCrary/D2L-MCP/internal/lms"
)

// Learner-side writes documented by Valence. Quiz attempts, retracting a
// submission and commenting on one's own submission have no API route.

// RichText is the dropbox submission comment block.
type RichText struct {
	Text string  `json:"Text"`
	Html *string `json:"Html"`
}

// RichTextInput is the request form used by discussion posts.
type RichTextInput struct {
	Content string `json:"Content"`
	Type    string `json:"Type"`
}

func richTextInput(content string, html bool) RichTextInput {
	if html {
		return RichTextInput{Content: content, Type: "Html"}
	}
	return RichTextInput{Content: content, Type: "Text"}
}

// Dropbox SubmissionType values; 2 (on paper) and 3 (observed) are not
// submitted online.
const (
	submissionFile       = 0
	submissionText       = 1
	submissionFileOrText = 4
)

type Submission struct {
	CourseID     int64    `json:"course_id"`
	FolderID     int64    `json:"folder_id"`
	GroupID      int64    `json:"group_id,omitempty"`
	Comment      string   `json:"comment,omitempty"`
	CommentHTML  bool     `json:"comment_html,omitempty"`
	Files        []string `json:"files"`
	ManagedRoot  string   `json:"-"`
	SubmittedAt  string   `json:"submitted_at"`
	Folder       string   `json:"folder"`
	Late         bool     `json:"late"`
	Verification any      `json:"verification,omitempty" jsonschema:"The user's submissions for this folder read back after submitting"`
	VerifyError  string   `json:"verify_error,omitempty"`
}

// FindAssignment resolves a folder name substring or numeric ID.
func (c *Client) FindAssignment(ctx context.Context, orgID int64, query string) (int64, error) {
	folders, err := c.Assignments(ctx, orgID)
	if err != nil {
		return 0, err
	}
	folder, err := matchNamedObject(folders, query, "Id", "Name", "assignment")
	if err != nil {
		return 0, err
	}
	return int64Field(folder, "Id"), nil
}

func (c *Client) Folder(ctx context.Context, orgID, folderID int64) (Object, error) {
	var out Object
	return out, c.Get(ctx, c.LE(fmt.Sprintf("/%d/dropbox/folders/%d", orgID, folderID)), nil, &out)
}

// Submit uploads files to a dropbox folder after checking the folder accepts
// files and the group context matches. Dates are left to Brightspace because
// special access can reopen a closed folder for one student.
func (c *Client) Submit(ctx context.Context, s Submission) (Submission, error) {
	if !c.writes {
		return s, writesDisabled()
	}
	if len(s.Files) == 0 {
		return s, lms.Invalid("Brightspace dropbox submissions require at least one file")
	}

	folder, err := c.Folder(ctx, s.CourseID, s.FolderID)
	if err != nil {
		return s, err
	}
	s.Folder = stringField(folder, "Name")

	if folder["IsHidden"] == true {
		return s, &Error{Code: ErrForbidden, Message: "This assignment folder is hidden from students."}
	}
	switch int64Field(folder, "SubmissionType") {
	case submissionFile, submissionFileOrText:
	case submissionText:
		return s, lms.Unsupported("text-only dropbox submissions; Valence documents file submissions only. Use the Brightspace web interface")
	default:
		return s, lms.Unsupported("on-paper or observed assignments; they are not submitted online")
	}

	isGroup := folder["GroupTypeId"] != nil
	switch {
	case isGroup && s.GroupID == 0:
		return s, lms.Invalid("This is a group assignment; supply group_id")
	case !isGroup && s.GroupID != 0:
		return s, lms.Invalid("This is an individual assignment; omit group_id")
	}

	files, closeFiles, err := OpenUploads(s.ManagedRoot, s.Files)
	if err != nil {
		return s, err
	}
	defer closeFiles()

	comment := RichText{Text: s.Comment}
	if s.CommentHTML {
		comment.Html = &s.Comment
	}
	parts := []Part{{JSON: comment}}
	for _, f := range files {
		parts = append(parts, Part{File: f})
	}

	path := c.LE(fmt.Sprintf("/%d/dropbox/folders/%d/submissions/mysubmissions/", s.CourseID, s.FolderID))
	if isGroup {
		path = c.LE(fmt.Sprintf("/%d/dropbox/folders/%d/submissions/group/%d/", s.CourseID, s.FolderID, s.GroupID))
	}

	now := time.Now().UTC()
	if err := c.WriteMultipart(ctx, "POST", path, parts, nil); err != nil {
		return s, err
	}
	s.SubmittedAt = FormatTime(now)
	if due := timeField(folder, "DueDate"); !due.IsZero() && now.After(due) {
		s.Late = true
	}

	// The submit response has no documented body; read back what Brightspace
	// recorded so the caller can confirm the files arrived.
	if verification, err := c.MySubmissions(ctx, s.CourseID, s.FolderID); err != nil {
		s.VerifyError = err.Error()
	} else {
		s.Verification = verification
	}

	return s, nil
}

type PostTarget struct {
	CourseID int64 `json:"course_id"`
	ForumID  int64 `json:"forum_id"`
	TopicID  int64 `json:"topic_id"`
}

func (t PostTarget) path(c *Client, suffix string) string {
	return c.LE(fmt.Sprintf("/%d/discussions/forums/%d/topics/%d/posts/%s", t.CourseID, t.ForumID, t.TopicID, suffix))
}

type NewPost struct {
	ParentPostID *int64
	Subject      string
	Message      string
	HTML         bool
	Anonymous    bool
	Files        []string
	ManagedRoot  string
}

// CreatePost starts a thread, or replies when ParentPostID is set. A reply
// without a subject reuses the parent's, as the Brightspace web UI does.
func (c *Client) CreatePost(ctx context.Context, t PostTarget, p NewPost) (Object, error) {
	if !c.writes {
		return nil, writesDisabled()
	}
	if strings.TrimSpace(p.Message) == "" {
		return nil, lms.Invalid("A post message is required")
	}

	subject := strings.TrimSpace(p.Subject)
	if subject == "" {
		if p.ParentPostID == nil {
			return nil, lms.Invalid("A new thread needs a subject")
		}
		var parent Object
		if err := c.Get(ctx, t.path(c, strconv.FormatInt(*p.ParentPostID, 10)), nil, &parent); err != nil {
			return nil, err
		}
		subject = stringField(parent, "Subject")
		if !strings.HasPrefix(strings.ToLower(subject), "re:") {
			subject = "Re: " + subject
		}
	}

	data := map[string]any{
		"ParentPostId": p.ParentPostID,
		"Subject":      subject,
		"Message":      richTextInput(p.Message, p.HTML),
		"IsAnonymous":  p.Anonymous,
	}

	var out Object
	if len(p.Files) == 0 {
		return out, c.Write(ctx, "POST", t.path(c, ""), data, &out)
	}

	files, closeFiles, err := OpenUploads(p.ManagedRoot, p.Files)
	if err != nil {
		return nil, err
	}
	defer closeFiles()
	parts := []Part{{JSON: data}}
	for i, f := range files {
		f.FieldName = "file " + strconv.Itoa(i)
		parts = append(parts, Part{File: f})
	}

	return out, c.WriteMultipart(ctx, "POST", t.path(c, ""), parts, &out)
}

// UpdatePost edits the user's own post. Valence takes a one-part multipart
// body here, unlike create.
func (c *Client) UpdatePost(ctx context.Context, t PostTarget, postID int64, subject, message string, html bool) (Object, error) {
	if strings.TrimSpace(message) == "" {
		return nil, lms.Invalid("A post message is required")
	}

	data := map[string]any{"Subject": subject, "Message": richTextInput(message, html)}
	var out Object

	return out, c.WriteMultipart(ctx, "PUT", t.path(c, strconv.FormatInt(postID, 10)), []Part{{JSON: data}}, &out)
}

func (c *Client) DeletePost(ctx context.Context, t PostTarget, postID int64) error {
	return c.Write(ctx, "DELETE", t.path(c, strconv.FormatInt(postID, 10)), nil, nil)
}

type PostState struct {
	Read    *bool `json:"read,omitempty" jsonschema:"Mark the post read or unread"`
	Flagged *bool `json:"flagged,omitempty" jsonschema:"Flag or unflag the post for yourself"`
	Rating  *int  `json:"rating,omitempty" jsonschema:"Your rating 1-5; 0 clears it. Only on topics with ratings enabled"`
}

// SetPostState applies each requested change in turn and reports which ones
// Brightspace accepted, since there is no single combined route.
func (c *Client) SetPostState(ctx context.Context, t PostTarget, postID int64, s PostState) (map[string]any, error) {
	type change struct {
		name, suffix string
		body         any
	}

	id := strconv.FormatInt(postID, 10)
	var changes []change
	if s.Read != nil {
		changes = append(changes, change{"read", id + "/ReadStatus", map[string]bool{"IsRead": *s.Read}})
	}
	if s.Flagged != nil {
		changes = append(changes, change{"flagged", id + "/Flag", map[string]bool{"IsFlagged": *s.Flagged}})
	}
	if s.Rating != nil {
		if *s.Rating < 0 || *s.Rating > 5 {
			return nil, lms.Invalid("rating must be 0 to 5")
		}
		changes = append(changes, change{"rating", id + "/Rating/MyRating", map[string]int{"Rating": *s.Rating}})
	}
	if len(changes) == 0 {
		return nil, lms.Invalid("Set at least one of read, flagged or rating")
	}

	applied := map[string]any{}
	for _, ch := range changes {
		var out any
		if err := c.Write(ctx, "PUT", t.path(c, ch.suffix), ch.body, &out); err != nil {
			if len(applied) == 0 {
				return nil, err
			}
			applied["error"] = fmt.Sprintf("%s failed after %d change(s) applied: %v", ch.name, len(applied), err)
			return applied, nil
		}
		applied[ch.name] = out
	}

	return applied, nil
}

// UpdateProgress marks a content topic visited, read or completed for the
// calling user. It reads the current progress first and changes only the
// requested fields, because the unstable route replaces the whole block.
func (c *Client) UpdateProgress(ctx context.Context, orgID, topicID, userID int64, read, completed *bool) (Object, error) {
	if !c.writes {
		return nil, writesDisabled()
	}
	if read == nil && completed == nil {
		return nil, lms.Invalid("Set read or completed")
	}

	query := map[string][]string{"userId": {strconv.FormatInt(userID, 10)}}
	var progress Object
	if err := c.Get(ctx, fmt.Sprintf("/d2l/api/le/unstable/%d/content/userprogress/%d", orgID, topicID), query, &progress); err != nil {
		return nil, err
	}
	if progress == nil {
		progress = Object{"ObjectId": topicID, "UserId": userID}
	}

	now := FormatTime(time.Now())
	progress["Visited"] = true
	progress["LastVisited"] = now
	if read != nil {
		progress["IsRead"] = *read
	}
	if completed != nil {
		progress["Completed"] = *completed
		progress["CompletedDate"] = nil
		if *completed {
			progress["CompletedDate"] = now
		}
	}

	var out Object
	path := fmt.Sprintf("/d2l/api/le/unstable/%d/content/userprogress/", orgID)

	return out, c.Write(ctx, "POST", path, progress, &out)
}
