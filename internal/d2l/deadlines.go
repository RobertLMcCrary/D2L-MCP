package d2l

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Deadlines are built from the course tools themselves: dropbox folders,
// quizzes and content due items. Instructors often leave DisplayInCalendar
// off, so the calendar only supplements what the courses already say.

const (
	StatusGraded       = "graded"
	StatusSubmitted    = "submitted"
	StatusNotSubmitted = "not_submitted"
	StatusCompleted    = "completed"
	StatusNotCompleted = "not_completed"
	StatusUnknown      = "unknown"
)

// Late items stay visible this long after their due date, even once closed.
const overdueLookback = 7 * 24 * time.Hour

type Deadline struct {
	CourseID   int64  `json:"course_id"`
	Course     string `json:"course"`
	Kind       string `json:"kind" jsonschema:"assignment quiz content or calendar"`
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Start      string `json:"start,omitempty"`
	Due        string `json:"due,omitempty"`
	Closes     string `json:"closes,omitempty" jsonschema:"When submissions stop being accepted"`
	Status     string `json:"status" jsonschema:"graded submitted not_submitted completed not_completed or unknown"`
	Overdue    bool   `json:"overdue,omitempty"`
	OnCalendar bool   `json:"on_calendar"`
	URL        string `json:"url,omitempty"`

	at time.Time
}

type DeadlineReport struct {
	From     string     `json:"from"`
	Until    string     `json:"until"`
	Items    []Deadline `json:"items"`
	Partial  bool       `json:"partial"`
	Warnings []string   `json:"warnings,omitempty"`
}

func (r DeadlineReport) IsPartial() bool { return r.Partial }

func (r *DeadlineReport) warn(course Course, source string, err error) {
	r.Partial = true
	r.Warnings = append(r.Warnings, fmt.Sprintf("%s %s: %v", course.Name, source, err))
}

type window struct{ now, until time.Time }

// includes reports whether an item belongs in the report: due or closing in
// the window, still open after its due date, or recently late.
func (w window) includes(due, closes time.Time) bool {
	in := func(t time.Time) bool { return !t.IsZero() && !t.Before(w.now) && !t.After(w.until) }
	switch {
	case in(due), in(closes):
		return true
	case !due.IsZero() && due.Before(w.now):
		return closes.After(w.now) || w.now.Sub(due) <= overdueLookback
	}
	return false
}

func (c *Client) Deadlines(ctx context.Context, courses []Course, now time.Time, days int) DeadlineReport {
	w := window{now: now.UTC(), until: now.UTC().Add(time.Duration(days) * 24 * time.Hour)}
	report := DeadlineReport{From: FormatTime(w.now), Until: FormatTime(w.until), Items: []Deadline{}}

	ids := make([]string, len(courses))
	for i, course := range courses {
		ids[i] = strconv.FormatInt(course.ID, 10)
	}

	// Content due items carry DateCompleted, the cheapest completion signal
	// for dropbox folders linked into course content.
	content := map[int64]map[string]Object{}
	dueItems, err := c.Due(ctx, strings.Join(ids, ","), w.now.Add(-overdueLookback), w.until)
	if err != nil {
		report.warn(Course{Name: "all courses"}, "content due items", err)
	}
	for _, item := range dueItems {
		orgID := int64Field(item, "OrgUnitId")
		if content[orgID] == nil {
			content[orgID] = map[string]Object{}
		}
		content[orgID][normalizeName(stringField(item, "ItemName"))] = item
	}

	for _, course := range courses {
		report.Items = append(report.Items, c.courseDeadlines(ctx, course, w, content[course.ID], &report)...)
	}

	sort.SliceStable(report.Items, func(i, j int) bool {
		return report.Items[i].at.Before(report.Items[j].at)
	})

	return report
}

func (c *Client) courseDeadlines(ctx context.Context, course Course, w window, content map[string]Object, report *DeadlineReport) []Deadline {
	var items []Deadline
	byEntity := map[string]int{}

	grades, err := c.Grades(ctx, course.ID)
	if err != nil {
		report.warn(course, "grades", err)
	}
	graded := gradedItems(grades)

	add := func(entity string, d Deadline) {
		d.CourseID, d.Course = course.ID, course.Name
		byEntity[entity] = len(items)
		items = append(items, d)
	}

	folders, err := c.Assignments(ctx, course.ID)
	if err != nil {
		report.warn(course, "assignments", err)
	}
	for _, folder := range folders {
		if folder["IsHidden"] == true {
			continue
		}
		availability, _ := folder["Availability"].(map[string]any)
		due, closes := timeField(folder, "DueDate"), timeField(availability, "EndDate")
		if !w.includes(due, closes) {
			continue
		}

		d := Deadline{
			Kind:   "assignment",
			ID:     int64Field(folder, "Id"),
			Name:   stringField(folder, "Name"),
			Start:  stringField(availability, "StartDate"),
			Due:    stringField(folder, "DueDate"),
			Closes: stringField(availability, "EndDate"),
			at:     firstTime(due, closes),
		}

		item, linked := takeLinked(content, d.Name)
		d.URL = stringField(item, "ItemUrl")
		switch {
		case graded[stringField(folder, "GradeItemId")]:
			d.Status = StatusGraded
		case linked && item["DateCompleted"] != nil:
			d.Status = StatusSubmitted
		default:
			d.Status = c.submissionStatus(ctx, course.ID, d.ID)
		}
		d.Overdue = d.Status == StatusNotSubmitted && !due.IsZero() && due.Before(w.now)
		add("dropbox:"+strconv.FormatInt(d.ID, 10), d)
	}

	quizzes, err := c.Quizzes(ctx, course.ID)
	if err != nil {
		report.warn(course, "quizzes", err)
	}
	for _, quiz := range quizzes {
		if quiz["IsActive"] == false {
			continue
		}
		due, closes := timeField(quiz, "DueDate"), timeField(quiz, "EndDate")
		if !w.includes(due, closes) {
			continue
		}

		// Learners cannot read quiz attempts, so only a released grade or
		// content completion proves the quiz was taken.
		d := Deadline{
			Kind:   "quiz",
			ID:     int64Field(quiz, "QuizId"),
			Name:   stringField(quiz, "Name"),
			Start:  stringField(quiz, "StartDate"),
			Due:    stringField(quiz, "DueDate"),
			Closes: stringField(quiz, "EndDate"),
			Status: StatusUnknown,
			at:     firstTime(due, closes),
		}
		item, linked := takeLinked(content, d.Name)
		d.URL = stringField(item, "ItemUrl")
		switch {
		case graded[stringField(quiz, "GradeItemId")]:
			d.Status = StatusGraded
		case linked && item["DateCompleted"] != nil:
			d.Status = StatusCompleted
		}
		add("quiz:"+strconv.FormatInt(d.ID, 10), d)
	}

	for _, item := range content {
		due, closes := timeField(item, "DueDate"), timeField(item, "EndDate")
		if !w.includes(due, closes) {
			continue
		}
		d := Deadline{
			Kind:   "content",
			ID:     int64Field(item, "ItemId"),
			Name:   stringField(item, "ItemName"),
			Start:  stringField(item, "StartDate"),
			Due:    stringField(item, "DueDate"),
			Closes: stringField(item, "EndDate"),
			Status: StatusNotCompleted,
			URL:    stringField(item, "ItemUrl"),
			at:     firstTime(due, closes),
		}
		if item["DateCompleted"] != nil {
			d.Status = StatusCompleted
		}
		d.Overdue = d.Status == StatusNotCompleted && !due.IsZero() && due.Before(w.now)
		add("content:"+strconv.FormatInt(d.ID, 10), d)
	}

	events, err := c.Calendar(ctx, &course.ID, "", w.now, w.until)
	if err != nil {
		report.warn(course, "calendar", err)
	}
	for _, event := range events {
		if entity := calendarEntity(event); entity != "" {
			if i, ok := byEntity[entity]; ok {
				items[i].OnCalendar = true
				continue
			}
		}
		start := timeField(event, "StartDateTime")
		add("event:"+stringField(event, "CalendarEventId"), Deadline{
			Kind:       "calendar",
			ID:         int64Field(event, "CalendarEventId"),
			Name:       stringField(event, "Title"),
			Start:      stringField(event, "StartDateTime"),
			Due:        stringField(event, "EndDateTime"),
			Status:     StatusUnknown,
			OnCalendar: true,
			URL:        stringField(event, "CalendarEventViewUrl"),
			at:         start,
		})
	}

	return items
}

func (c *Client) submissionStatus(ctx context.Context, orgID, folderID int64) string {
	entries, err := c.MySubmissions(ctx, orgID, folderID)
	if err != nil {
		// Closed folders commonly deny this read; that is not "missing".
		return StatusUnknown
	}
	for _, entry := range entries {
		if submissions, ok := entry["Submissions"].([]any); ok && len(submissions) > 0 {
			return StatusSubmitted
		}
	}
	return StatusNotSubmitted
}

// gradedItems returns grade object IDs that have a released score.
func gradedItems(grades any) map[string]bool {
	graded := map[string]bool{}
	list, _ := grades.([]any)
	for _, raw := range list {
		if grade, ok := raw.(map[string]any); ok && grade["PointsNumerator"] != nil {
			graded[stringField(grade, "GradeObjectIdentifier")] = true
		}
	}
	return graded
}

// takeLinked removes and returns the content due item that shares a name with
// a folder or quiz, so it is not reported a second time as plain content.
func takeLinked(content map[string]Object, name string) (Object, bool) {
	key := normalizeName(name)
	item, ok := content[key]
	delete(content, key)
	return item, ok
}

func calendarEntity(event Object) string {
	associated, ok := event["AssociatedEntity"].(map[string]any)
	if !ok {
		return ""
	}
	id := stringField(associated, "AssociatedEntityId")
	kind := stringField(associated, "AssociatedEntityType")
	switch {
	case strings.Contains(kind, "Dropbox"):
		return "dropbox:" + id
	case strings.Contains(kind, "Quiz"):
		return "quiz:" + id
	}
	return ""
}

func normalizeName(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), " "))
}

func stringField(object map[string]any, key string) string {
	switch v := object[key].(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return ""
}

func int64Field(object map[string]any, key string) int64 {
	id, _ := strconv.ParseInt(stringField(object, key), 10, 64)
	return id
}

func timeField(object map[string]any, key string) time.Time {
	t, _ := time.Parse(time.RFC3339, stringField(object, key))
	return t
}

func firstTime(times ...time.Time) time.Time {
	for _, t := range times {
		if !t.IsZero() {
			return t
		}
	}
	return time.Time{}
}
