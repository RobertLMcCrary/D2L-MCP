package canvas

import (
	"context"
	"encoding/json"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RobertLMcCrary/D2L-MCP/internal/lms"
	"golang.org/x/sync/errgroup"
)

// ResolveCourse never guesses when a name or code matches multiple courses.
func (c *Client) ResolveCourse(ctx context.Context, query string) (json.RawMessage, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, lms.Invalid("Course query is required")
	}
	response, err := c.Read(ctx, "list_courses", nil, nil)
	if err != nil {
		return nil, err
	}
	var courses []json.RawMessage
	if err := json.Unmarshal(response.Data, &courses); err != nil {
		return nil, err
	}
	exact, partial := []json.RawMessage{}, []json.RawMessage{}
	for _, raw := range courses {
		var course Course
		if err := json.Unmarshal(raw, &course); err != nil {
			return nil, err
		}
		if string(course.ID) == query {
			return raw, nil
		}
		if strings.EqualFold(course.Name, query) || strings.EqualFold(course.Code, query) {
			exact = append(exact, raw)
		} else if strings.Contains(strings.ToLower(course.Name), strings.ToLower(query)) || strings.Contains(strings.ToLower(course.Code), strings.ToLower(query)) {
			partial = append(partial, raw)
		}
	}
	matches := exact
	if len(matches) == 0 {
		matches = partial
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return nil, &lms.Error{Code: "ambiguous_match", Message: "Multiple courses match; use the exact course ID from list_courses"}
	}
	return nil, &lms.Error{Code: "not_found", Message: "No enrolled course matches"}
}

type Snapshot struct {
	GeneratedAt time.Time                  `json:"generated_at"`
	Data        map[string]json.RawMessage `json:"data"`
	Partial     bool                       `json:"partial"`
	Warnings    map[string]string          `json:"warnings,omitempty"`
}

func (s Snapshot) IsPartial() bool { return s.Partial }

func (c *Client) Snapshot(ctx context.Context, courseID string) (Snapshot, error) {
	if courseID == "" {
		return Snapshot{}, lms.Invalid("course_id is required")
	}
	result := Snapshot{GeneratedAt: time.Now().UTC(), Data: map[string]json.RawMessage{}, Warnings: map[string]string{}}
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(4)
	var mu sync.Mutex
	for _, name := range []string{"get_course", "get_grades", "list_assignments", "list_modules", "list_announcements", "list_discussions", "list_quizzes"} {
		group.Go(func() error {
			response, err := c.Read(groupCtx, name, map[string]string{"course_id": courseID}, nil)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				result.Partial = true
				result.Warnings[name] = err.Error()
			} else {
				result.Data[name] = response.Data
			}
			return nil
		})
	}
	_ = group.Wait()
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	return result, nil
}

type Deadline struct {
	CourseID     string    `json:"course_id"`
	AssignmentID string    `json:"assignment_id"`
	Name         string    `json:"name"`
	DueAt        time.Time `json:"due_at"`
	State        string    `json:"state"`
	Missing      bool      `json:"missing"`
	Late         bool      `json:"late"`
	Locked       bool      `json:"locked"`
	RedoRequest  bool      `json:"redo_request"`
	URL          string    `json:"url,omitempty"`
}

type DeadlineReport struct {
	GeneratedAt time.Time         `json:"generated_at"`
	Items       []Deadline        `json:"items"`
	Partial     bool              `json:"partial"`
	Warnings    map[string]string `json:"warnings,omitempty"`
}

func (r DeadlineReport) IsPartial() bool { return r.Partial }

// Deadlines reads assignments with each student's effective due date and own
// submission. A late, submitted assignment is not mislabeled as overdue.
func (c *Client) Deadlines(ctx context.Context, courseIDs []string, days int, overdue bool) (DeadlineReport, error) {
	if days <= 0 || days > 365 {
		return DeadlineReport{}, lms.Invalid("days must be between 1 and 365")
	}
	if len(courseIDs) == 0 {
		r, err := c.Read(ctx, "list_courses", nil, url.Values{"enrollment_state": {"active"}})
		if err != nil {
			return DeadlineReport{}, err
		}
		var courses []Course
		if err := json.Unmarshal(r.Data, &courses); err != nil {
			return DeadlineReport{}, err
		}
		for _, course := range courses {
			courseIDs = append(courseIDs, string(course.ID))
		}
	}
	if len(courseIDs) > 100 {
		return DeadlineReport{}, lms.Invalid("At most 100 courses per deadline report")
	}
	now := time.Now().UTC()
	end := now.Add(time.Duration(days) * 24 * time.Hour)
	report := DeadlineReport{GeneratedAt: now, Items: []Deadline{}, Warnings: map[string]string{}}
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(4)
	var mu sync.Mutex
	seen := map[string]bool{}
	for _, id := range courseIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		group.Go(func() error {
			r, err := c.Read(groupCtx, "list_assignments", map[string]string{"course_id": id}, nil)
			var assignments []Assignment
			if err == nil {
				err = json.Unmarshal(r.Data, &assignments)
			}
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				report.Partial = true
				report.Warnings[id] = err.Error()
				return nil
			}
			for _, a := range assignments {
				s := a.Submission
				if s == nil {
					report.Partial = true
					report.Warnings[id] = "Some assignments omitted own submission status; their deadline state is unknown"
					continue
				}
				if a.DueAt == nil || s.Excused {
					continue
				}
				// Canvas can grade missing work automatically, or reassign already
				// submitted work. These explicit signals outrank completion state.
				needsWork := s.Missing || s.RedoRequest
				if !needsWork && (s.SubmittedAt != nil || s.State == Submitted || s.State == PendingReview || s.State == Graded) {
					continue
				}
				past := a.DueAt.Before(now)
				if overdue && !past || !overdue && (past || a.DueAt.After(end)) {
					continue
				}
				state := "upcoming"
				if past {
					state = "overdue"
				}
				if s.Missing {
					state = "missing"
				}
				if s.RedoRequest {
					state = "reassigned"
				}
				report.Items = append(report.Items, Deadline{CourseID: id, AssignmentID: string(a.ID), Name: a.Name, DueAt: *a.DueAt, State: state, Missing: s.Missing, Late: s.Late, Locked: a.LockedForUser, RedoRequest: s.RedoRequest, URL: a.HTMLURL})
			}
			return nil
		})
	}
	_ = group.Wait()
	if err := ctx.Err(); err != nil {
		return DeadlineReport{}, err
	}
	sort.Slice(report.Items, func(i, j int) bool {
		if report.Items[i].DueAt.Equal(report.Items[j].DueAt) {
			return report.Items[i].AssignmentID < report.Items[j].AssignmentID
		}
		return report.Items[i].DueAt.Before(report.Items[j].DueAt)
	})
	return report, nil
}

type SubmissionInput struct {
	CourseID     string         `json:"course_id"`
	AssignmentID string         `json:"assignment_id"`
	Type         SubmissionType `json:"type" jsonschema:"online_text_entry online_url or online_upload"`
	Text         string         `json:"text,omitempty"`
	URL          string         `json:"url,omitempty"`
	FileIDs      []string       `json:"file_ids,omitempty"`
	Comment      string         `json:"comment,omitempty"`
}

func (c *Client) Submit(ctx context.Context, input SubmissionInput) (lms.Response, error) {
	if !c.writes {
		return lms.Response{}, &lms.Error{Code: "writes_disabled", Message: "Enable Canvas writes before submitting"}
	}
	sub := map[string]any{"submission_type": input.Type}
	switch input.Type {
	case "online_text_entry":
		if strings.TrimSpace(input.Text) == "" {
			return lms.Response{}, lms.Invalid("Submission text is required")
		}
		sub["body"] = input.Text
	case "online_url":
		u, err := url.Parse(input.URL)
		if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
			return lms.Response{}, lms.Invalid("A valid submission URL is required")
		}
		sub["url"] = input.URL
	case "online_upload":
		if len(input.FileIDs) == 0 {
			return lms.Response{}, lms.Invalid("At least one uploaded file ID is required")
		}
		for _, id := range input.FileIDs {
			if id == "" {
				return lms.Response{}, lms.Invalid("Empty file ID")
			}
		}
		sub["file_ids"] = input.FileIDs
	default:
		return lms.Response{}, lms.Unsupported("this submission type; use the Canvas web interface for LTI, media, quizzes, or in-person work")
	}
	r, err := c.Read(ctx, "get_assignment", map[string]string{"course_id": input.CourseID, "assignment_id": input.AssignmentID}, nil)
	if err != nil {
		return lms.Response{}, err
	}
	var assignment Assignment
	if err := json.Unmarshal(r.Data, &assignment); err != nil {
		return lms.Response{}, err
	}
	if assignment.LockedForUser {
		return lms.Response{}, lms.Invalid("This assignment is locked for the current user")
	}
	allowed := false
	for _, typ := range assignment.SubmissionTypes {
		if typ == input.Type {
			allowed = true
		}
	}
	if !allowed {
		return lms.Response{}, lms.Invalid("This assignment does not accept the requested submission type")
	}
	if assignment.AllowedAttempts != nil && *assignment.AllowedAttempts >= 0 {
		attempt, extra := 0, 0
		if assignment.Submission != nil {
			attempt = assignment.Submission.Attempt
			extra = assignment.Submission.ExtraAttempts
		}
		if attempt >= *assignment.AllowedAttempts+extra {
			return lms.Response{}, lms.Invalid("This assignment's submission attempts are exhausted")
		}
	}
	body := map[string]any{"submission": sub}
	if input.Comment != "" {
		body["comment"] = map[string]any{"text_comment": input.Comment}
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return lms.Response{}, err
	}
	op := Operation{Path: "/api/v1/courses/{course_id}/assignments/{assignment_id}/submissions"}
	request, err := op.Request(map[string]string{"course_id": input.CourseID, "assignment_id": input.AssignmentID}, nil)
	if err != nil {
		return lms.Response{}, err
	}
	request.Method = "POST"
	request.Body = encoded
	return c.Do(ctx, request)
}
