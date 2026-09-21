package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RobertLMcCrary/D2L-MCP/internal/auth"
	"github.com/RobertLMcCrary/D2L-MCP/internal/config"
	"github.com/RobertLMcCrary/D2L-MCP/internal/d2l"
)

type Service struct {
	Paths  config.Paths
	Config config.Config

	mu       sync.Mutex
	token    string
	client   *d2l.Client
	resolver *d2l.Resolver
}

func Load() (*Service, error) {
	paths, err := config.DefaultPaths()
	if err != nil {
		return nil, err
	}

	cfg, err := config.Load(paths)
	if err != nil {
		return nil, err
	}

	if cfg.LMSHost != "" {
		if err := config.Validate(&cfg, false); err != nil {
			return nil, err
		}
	}

	return &Service{Paths: paths, Config: cfg}, nil
}

func (s *Service) Client(ctx context.Context) (*d2l.Client, *d2l.Resolver, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Config.LMSHost == "" {
		return nil, nil, &d2l.Error{
			Code:     d2l.ErrConfig,
			Message:  "No Brightspace school is configured.",
			NextStep: "Run: d2l-mcp setup --host https://your-school.example",
		}
	}

	token, _, err := auth.Load(s.Paths)
	if err != nil {
		refreshCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
		_, refreshErr := auth.SilentRefresh(refreshCtx, s.Paths, s.Config.LMSHost)
		cancel()
		if refreshErr != nil {
			return nil, nil, err
		}
		token, _, err = auth.Load(s.Paths)
		if err != nil {
			return nil, nil, err
		}
	}

	if s.client != nil && token == s.token {
		return s.client, s.resolver, nil
	}

	client, err := d2l.NewClient(s.Config.LMSHost, token)
	if err != nil {
		return nil, nil, err
	}

	s.token = token
	s.client = client
	s.resolver = d2l.NewResolver(client)

	return s.client, s.resolver, nil
}

func (s *Service) Resolve(ctx context.Context, query string) (*d2l.Client, d2l.Course, error) {
	client, resolver, err := s.Client(ctx)
	if err != nil {
		return nil, d2l.Course{}, err
	}

	course, err := resolver.Resolve(ctx, query)

	return client, course, err
}

func (s *Service) Downloader(client *d2l.Client) *d2l.Downloader {
	return d2l.NewDownloader(client, config.DownloadRoot(s.Paths, s.Config))
}

type Status struct {
	Configured    bool        `json:"configured"`
	LMSHost       string      `json:"lms_host,omitempty"`
	SyllabusHost  string      `json:"syllabus_host,omitempty"`
	Token         auth.Status `json:"token"`
	APIReachable  bool        `json:"api_reachable"`
	User          d2l.Object  `json:"user,omitempty"`
	ActiveCourses int         `json:"active_courses,omitempty"`
	NextStep      string      `json:"next_step,omitempty"`
	Checks        []Check     `json:"checks"`
}

type Check struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Details string `json:"details,omitempty"`
}

func (s *Service) Status(ctx context.Context, network bool) Status {
	result := Status{
		Configured:   s.Config.LMSHost != "",
		LMSHost:      s.Config.LMSHost,
		SyllabusHost: s.Config.SyllabusHost,
	}

	result.Checks = append(result.Checks, Check{
		Name:    "config",
		OK:      result.Configured,
		Details: s.Config.LMSHost,
	})

	if !result.Configured {
		result.NextStep = "d2l-mcp setup --host https://your-school.example"
		return result
	}

	_, tokenStatus, err := auth.Load(s.Paths)
	result.Token = tokenStatus
	result.Checks = append(result.Checks, Check{
		Name:    "token",
		OK:      err == nil,
		Details: tokenStatus.ExpiresAt.Format(time.RFC3339),
	})

	if err != nil {
		result.NextStep = "d2l-mcp login"
		return result
	}

	if !network {
		return result
	}

	client, resolver, err := s.Client(ctx)
	if err != nil {
		result.NextStep = "d2l-mcp login"
		result.Checks = append(result.Checks, Check{
			Name:    "api",
			OK:      false,
			Details: err.Error(),
		})

		return result
	}

	result.User, err = client.WhoAmI(ctx)
	result.APIReachable = err == nil
	result.Checks = append(result.Checks, Check{Name: "api", OK: err == nil})

	if err != nil {
		result.NextStep = "d2l-mcp doctor"
		return result
	}

	courses, err := resolver.Courses(ctx, false)
	if err == nil {
		result.ActiveCourses = len(courses)
	}

	result.Checks = append(result.Checks, Check{
		Name:    "courses",
		OK:      err == nil,
		Details: fmt.Sprintf("%d active course(s)", len(courses)),
	})

	return result
}

type Snapshot struct {
	GeneratedAt string           `json:"generated_at"`
	Student     d2l.Object       `json:"student"`
	Overdue     []d2l.Object     `json:"overdue_items"`
	DueSoon     []d2l.Object     `json:"due_soon"`
	Courses     []CourseSnapshot `json:"courses,omitempty"`
	Partial     bool             `json:"partial"`
	Warnings    []string         `json:"warnings,omitempty"`
}

type CourseSnapshot struct {
	Course      d2l.Course   `json:"course"`
	Grades      any          `json:"grades,omitempty"`
	Assignments []d2l.Object `json:"assignments,omitempty"`
	Quizzes     []d2l.Object `json:"quizzes,omitempty"`
	News        []d2l.Object `json:"news,omitempty"`
	Calendar    []d2l.Object `json:"calendar,omitempty"`
	ContentTOC  any          `json:"content_toc,omitempty"`
	Forums      []d2l.Object `json:"discussion_forums,omitempty"`
}

func (s *Service) Snapshot(ctx context.Context, queries []string, shallow bool, sinceHours float64) (Snapshot, error) {
	client, resolver, err := s.Client(ctx)
	if err != nil {
		return Snapshot{}, err
	}

	user, err := client.WhoAmI(ctx)
	if err != nil {
		return Snapshot{}, err
	}

	var courses []d2l.Course

	if len(queries) == 0 {
		courses, err = resolver.Courses(ctx, false)
	} else {
		for _, query := range queries {
			course, resolveErr := resolver.Resolve(ctx, query)
			if resolveErr != nil {
				return Snapshot{}, resolveErr
			}
			courses = append(courses, course)
		}
	}

	if err != nil {
		return Snapshot{}, err
	}

	ids := courseIDs(courses)
	now := time.Now().UTC()
	end := now.Add(7 * 24 * time.Hour)

	overdue, overdueErr := client.Overdue(ctx, ids)
	due, dueErr := client.Due(
		ctx,
		ids,
		now.Format("2006-01-02T15:04:05.000Z"),
		end.Format("2006-01-02T15:04:05.000Z"),
	)

	result := Snapshot{
		GeneratedAt: now.Format(time.RFC3339),
		Student:     user,
		Overdue:     overdue,
		DueSoon:     due,
	}

	for _, fetchErr := range []error{overdueErr, dueErr} {
		if fetchErr != nil {
			result.Partial = true
			result.Warnings = append(result.Warnings, fetchErr.Error())
		}
	}

	if shallow {
		return result, nil
	}

	since := ""

	if sinceHours > 0 {
		since = now.Add(-time.Duration(sinceHours * float64(time.Hour))).Format("2006-01-02T15:04:05.000Z")
	}

	for _, course := range courses {
		entry := CourseSnapshot{Course: course}

		entry.Grades, err = client.Grades(ctx, course.ID)
		recordWarning(&result, err)

		entry.Assignments, err = client.Assignments(ctx, course.ID)
		recordWarning(&result, err)

		entry.Quizzes, err = client.Quizzes(ctx, course.ID)
		recordWarning(&result, err)

		entry.News, err = client.News(ctx, course.ID, since)
		recordWarning(&result, err)

		calendarEnd := now.Add(14 * 24 * time.Hour).Format("2006-01-02T15:04:05.000Z")
		entry.Calendar, err = client.Calendar(ctx, &course.ID, since, calendarEnd)
		recordWarning(&result, err)

		if since == "" {
			entry.ContentTOC, err = client.ContentTOC(ctx, course.ID)
			recordWarning(&result, err)

			entry.Forums, err = client.Forums(ctx, course.ID)
			recordWarning(&result, err)
		}

		result.Courses = append(result.Courses, entry)
	}

	return result, nil
}

func courseIDs(courses []d2l.Course) string {
	ids := make([]string, len(courses))
	for i, course := range courses {
		ids[i] = strconv.FormatInt(course.ID, 10)
	}
	return strings.Join(ids, ",")
}

func recordWarning(snapshot *Snapshot, err error) {
	if err != nil {
		snapshot.Partial = true
		snapshot.Warnings = append(snapshot.Warnings, err.Error())
	}
}
