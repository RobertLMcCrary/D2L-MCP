package mcpserver

import (
	"context"
	"strconv"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/RobertLMcCrary/D2L-MCP/internal/app"
	"github.com/RobertLMcCrary/D2L-MCP/internal/config"
	"github.com/RobertLMcCrary/D2L-MCP/internal/d2l"
	"github.com/RobertLMcCrary/D2L-MCP/internal/lms"
)

type SubmitInput struct {
	Course      string   `json:"course" jsonschema:"Course name code or numeric org unit ID"`
	Assignment  string   `json:"assignment" jsonschema:"Assignment folder name substring or numeric ID"`
	Files       []string `json:"files" jsonschema:"Paths relative to the managed download directory; only files the user selected"`
	Comment     string   `json:"comment,omitempty" jsonschema:"Optional comment shown to the instructor with the submission"`
	CommentHTML bool     `json:"comment_html,omitempty" jsonschema:"Treat comment as HTML"`
	GroupID     int64    `json:"group_id,omitempty" jsonschema:"Required for group assignments only"`
}

type PostLocation struct {
	Course  string `json:"course" jsonschema:"Course name code or numeric org unit ID"`
	ForumID int64  `json:"forum_id"`
	TopicID int64  `json:"topic_id"`
}

type CreatePostInput struct {
	PostLocation
	ParentPostID int64    `json:"parent_post_id,omitempty" jsonschema:"Post to reply to; omit to start a new thread"`
	Subject      string   `json:"subject,omitempty" jsonschema:"Required for a new thread; replies default to Re: <parent subject>"`
	Message      string   `json:"message"`
	HTML         bool     `json:"html,omitempty" jsonschema:"Treat message as HTML"`
	Anonymous    bool     `json:"anonymous,omitempty" jsonschema:"Post anonymously where the topic allows it"`
	Files        []string `json:"files,omitempty" jsonschema:"Attachments relative to the managed download directory"`
}

type EditPostInput struct {
	PostLocation
	PostID  int64  `json:"post_id"`
	Subject string `json:"subject"`
	Message string `json:"message"`
	HTML    bool   `json:"html,omitempty" jsonschema:"Treat message as HTML"`
}

type PostIDInput struct {
	PostLocation
	PostID int64 `json:"post_id"`
}

type PostStateInput struct {
	PostLocation
	PostID int64 `json:"post_id"`
	d2l.PostState
}

type ProgressInput struct {
	Course    string `json:"course" jsonschema:"Course name code or numeric org unit ID"`
	TopicID   int64  `json:"topic_id" jsonschema:"Numeric content topic ID"`
	Read      *bool  `json:"read,omitempty" jsonschema:"Mark the topic read or unread"`
	Completed *bool  `json:"completed,omitempty" jsonschema:"Mark the topic complete or incomplete"`
}

// registerD2LWrites adds learner write tools. Each still passes the client
// write gate, so registration alone never enables a write.
func registerD2LWrites(s *server.MCPServer, service *app.Service) {
	if !service.Config.EnableWrites {
		return
	}

	root := func() string { return config.DownloadRoot(service.Paths, service.Config) }
	target := func(ctx context.Context, at PostLocation) (*d2l.Client, d2l.PostTarget, error) {
		client, course, err := service.Resolve(ctx, at.Course)
		return client, d2l.PostTarget{CourseID: course.ID, ForumID: at.ForumID, TopicID: at.TopicID}, err
	}

	addTool(s, "d2l_submit_assignment",
		"Submit user-selected files to an assignment dropbox folder, with an optional comment. Only on the user's explicit "+
			"request. Brightspace cannot retract a submission through the API, and folders that keep only one submission "+
			"reject a second. Checks that the folder accepts files, then reads back the recorded submission. Never retry "+
			"a failed submission automatically; inspect d2l_get_deadlines or the folder first.",
		SubmitInput{}, Output[d2l.Submission]{}, writeAnnotations(),
		func(ctx context.Context, input SubmitInput) (d2l.Submission, error) {
			client, course, err := service.Resolve(ctx, input.Course)
			if err != nil {
				return d2l.Submission{}, err
			}
			folder, err := client.FindAssignment(ctx, course.ID, input.Assignment)
			if err != nil {
				return d2l.Submission{}, err
			}
			return client.Submit(ctx, d2l.Submission{
				CourseID:    course.ID,
				FolderID:    folder,
				GroupID:     input.GroupID,
				Comment:     input.Comment,
				CommentHTML: input.CommentHTML,
				Files:       input.Files,
				ManagedRoot: root(),
			})
		})

	addTool(s, "d2l_post_discussion",
		"Start a discussion thread or reply to a post, optionally with attachments. Only post text the user wrote or "+
			"approved. Group-type topics are not supported by the API.",
		CreatePostInput{}, Output[d2l.Object]{}, writeAnnotations(),
		func(ctx context.Context, input CreatePostInput) (d2l.Object, error) {
			client, t, err := target(ctx, input.PostLocation)
			if err != nil {
				return nil, err
			}
			post := d2l.NewPost{
				Subject:     input.Subject,
				Message:     input.Message,
				HTML:        input.HTML,
				Anonymous:   input.Anonymous,
				Files:       input.Files,
				ManagedRoot: root(),
			}
			if input.ParentPostID != 0 {
				post.ParentPostID = &input.ParentPostID
			}
			return client.CreatePost(ctx, t, post)
		})

	addTool(s, "d2l_edit_discussion_post",
		"Replace the subject and message of the user's own discussion post.",
		EditPostInput{}, Output[d2l.Object]{}, writeAnnotations(),
		func(ctx context.Context, input EditPostInput) (d2l.Object, error) {
			client, t, err := target(ctx, input.PostLocation)
			if err != nil {
				return nil, err
			}
			return client.UpdatePost(ctx, t, input.PostID, input.Subject, input.Message, input.HTML)
		})

	addTool(s, "d2l_delete_discussion_post",
		"Delete the user's own discussion post. Cannot be undone; confirm with the user first.",
		PostIDInput{}, Output[map[string]any]{}, destructiveAnnotations(),
		func(ctx context.Context, input PostIDInput) (map[string]any, error) {
			client, t, err := target(ctx, input.PostLocation)
			if err != nil {
				return nil, err
			}
			if err := client.DeletePost(ctx, t, input.PostID); err != nil {
				return nil, err
			}
			return map[string]any{"deleted_post_id": input.PostID}, nil
		})

	addTool(s, "d2l_set_discussion_post_state",
		"Mark a discussion post read or unread, flag or unflag it, or set your 1-5 rating (0 clears it).",
		PostStateInput{}, Output[map[string]any]{}, writeAnnotations(),
		func(ctx context.Context, input PostStateInput) (map[string]any, error) {
			client, t, err := target(ctx, input.PostLocation)
			if err != nil {
				return nil, err
			}
			return client.SetPostState(ctx, t, input.PostID, input.PostState)
		})

	addTool(s, "d2l_update_content_progress",
		"Mark one content topic read or completed for the user. Uses Brightspace's unstable progress route, which only "+
			"updates the caller's own progress. Use it only when the user asks; completion can affect release conditions.",
		ProgressInput{}, Output[d2l.Object]{}, writeAnnotations(),
		func(ctx context.Context, input ProgressInput) (d2l.Object, error) {
			client, course, err := service.Resolve(ctx, input.Course)
			if err != nil {
				return nil, err
			}
			me, err := client.WhoAmI(ctx)
			if err != nil {
				return nil, err
			}
			identifier, _ := me["Identifier"].(string)
			userID, err := strconv.ParseInt(identifier, 10, 64)
			if err != nil {
				return nil, &lms.Error{Code: "INVALID_RESPONSE", Message: "Brightspace did not return a numeric user ID."}
			}
			return client.UpdateProgress(ctx, course.ID, input.TopicID, userID, input.Read, input.Completed)
		})
}

func destructiveAnnotations() []mcp.ToolOption {
	return append(writeAnnotations(), mcp.WithDestructiveHintAnnotation(true))
}
