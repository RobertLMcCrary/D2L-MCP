package d2l

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type Course struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Code   string `json:"code,omitempty"`
	Type   string `json:"type,omitempty"`
	Access any    `json:"access,omitempty"`
	Raw    Object `json:"raw,omitempty"`
}

type Resolver struct {
	client      *Client
	mu          sync.Mutex
	enrollments []Course
}

func NewResolver(client *Client) *Resolver {
	return &Resolver{client: client}
}

func (r *Resolver) Courses(ctx context.Context, includeInactive bool) ([]Course, error) {
	if includeInactive {
		records, err := r.client.Enrollments(ctx, false)
		if err != nil {
			return nil, err
		}

		return parseCourses(records, true), nil
	}

	return r.load(ctx)
}

func (r *Resolver) Resolve(ctx context.Context, query string) (Course, error) {
	// Resolution priority mirrors d2l-cli v0.2.2:
	// https://github.com/Aaryan-Kapoor/d2l-cli/blob/v0.2.2/src/d2l/resolver.py
	courses, err := r.load(ctx)
	if err != nil {
		return Course{}, err
	}

	query = strings.TrimSpace(query)
	lower := strings.ToLower(query)

	if id, err := strconv.ParseInt(query, 10, 64); err == nil {
		for _, course := range courses {
			if course.ID == id {
				return course, nil
			}
		}

		return Course{}, &Error{
			Code:    ErrNotFound,
			Message: fmt.Sprintf("No enrollment found with org unit ID %d.", id),
		}
	}

	for _, course := range courses {
		if course.Code != "" && strings.EqualFold(course.Code, query) {
			return course, nil
		}
	}

	var matches []Course

	for _, course := range courses {
		if strings.Contains(strings.ToLower(course.Name), lower) {
			matches = append(matches, course)
		}
	}

	if len(matches) == 1 {
		return matches[0], nil
	}

	if len(matches) > 1 {
		return Course{}, ambiguous(query, matches)
	}

	words := wordSet(lower)

	type scored struct {
		score  int
		course Course
	}

	var scores []scored

	for _, course := range courses {
		score := 0

		for word := range wordSet(strings.ToLower(course.Name)) {
			if words[word] {
				score++
			}
		}

		if score > 0 {
			scores = append(scores, scored{score, course})
		}
	}

	sort.SliceStable(scores, func(i, j int) bool {
		return scores[i].score > scores[j].score
	})

	if len(scores) > 0 {
		best := scores[0].score
		matches = nil

		for _, item := range scores {
			if item.score == best {
				matches = append(matches, item.course)
			}
		}

		if len(matches) == 1 {
			return matches[0], nil
		}

		return Course{}, ambiguous(query, matches)
	}

	return Course{}, &Error{
		Code:     ErrNotFound,
		Message:  fmt.Sprintf("No course matching %q.", query),
		NextStep: "Call d2l_list_courses to see available courses.",
	}
}

func (r *Resolver) load(ctx context.Context) ([]Course, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.enrollments != nil {
		return append([]Course(nil), r.enrollments...), nil
	}

	records, err := r.client.Enrollments(ctx, true)
	if err != nil {
		return nil, err
	}

	r.enrollments = parseCourses(records, false)

	return append([]Course(nil), r.enrollments...), nil
}

func parseCourses(records []Object, allTypes bool) []Course {
	var courses []Course

	for _, record := range records {
		org, _ := record["OrgUnit"].(map[string]any)
		typeMap, _ := org["Type"].(map[string]any)
		typeName, _ := typeMap["Name"].(string)

		if !allTypes && typeName != "Course Offering" {
			continue
		}

		id, _ := numberInt64(org["Id"])
		name, _ := org["Name"].(string)
		code, _ := org["Code"].(string)

		courses = append(courses, Course{
			ID:     id,
			Name:   name,
			Code:   code,
			Type:   typeName,
			Access: record["Access"],
			Raw:    record,
		})
	}

	return courses
}

func ambiguous(query string, matches []Course) error {
	options := make([]string, 0, len(matches))
	for _, match := range matches {
		options = append(options, fmt.Sprintf("[%d] %s", match.ID, match.Name))
	}

	return &Error{
		Code:     ErrAmbiguous,
		Message:  fmt.Sprintf("Multiple courses match %q: %s", query, strings.Join(options, "; ")),
		NextStep: "Use the numeric course ID.",
	}
}

func numberInt64(value any) (int64, bool) {
	switch n := value.(type) {
	case float64:
		return int64(n), true
	case int64:
		return n, true
	case jsonNumber:
		value, err := strconv.ParseInt(string(n), 10, 64)
		return value, err == nil
	}
	return 0, false
}

type jsonNumber string

func wordSet(value string) map[string]bool {
	result := map[string]bool{}
	for _, word := range strings.Fields(value) {
		result[word] = true
	}

	return result
}
