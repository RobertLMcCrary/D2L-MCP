package d2l

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestCalendarSendsMillisecondDatesAndCourseIDs(t *testing.T) {
	client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("orgUnitIdsCSV") != "1,2" {
			t.Errorf("orgUnitIdsCSV = %q, want 1,2", query.Get("orgUnitIdsCSV"))
		}
		if query.Get("startDateTime") != "2026-10-15T12:00:00.000Z" {
			t.Errorf("startDateTime = %q, want millisecond UTCDateTime", query.Get("startDateTime"))
		}
		if query.Get("bookmark") == "" {
			_, _ = w.Write([]byte(`{"Objects":[{"CalendarEventId":1}],"Next":"/d2l/api/le/1.80/calendar/events/myEvents/?bookmark=2&orgUnitIdsCSV=1,2&startDateTime=2026-10-15T12:00:00.000Z"}`))
			return
		}
		_, _ = w.Write([]byte(`{"Objects":[{"CalendarEventId":2}],"Next":null}`))
	}))

	start := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	events, err := client.Calendar(t.Context(), nil, "1,2", start, start.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2 across pages", len(events))
	}
}

func TestCalendarWithoutCoursesSkipsRequest(t *testing.T) {
	client := testClient(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("global calendar without course IDs is rejected by Brightspace and must not be sent")
	}))

	events, err := client.Calendar(t.Context(), nil, "", time.Now(), time.Now())
	if err != nil || len(events) != 0 {
		t.Fatalf("events = %v, err = %v; want empty", events, err)
	}
}

func TestPaginationRejectsCrossOriginNext(t *testing.T) {
	client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"Objects":[],"Next":"https://evil.example/d2l/api/le/1.80/x"}`))
	}))

	if _, err := client.Due(t.Context(), "1", time.Now(), time.Now()); err == nil {
		t.Fatal("cross-origin Next link was followed")
	}
}

func TestListShapeMismatchIsAnError(t *testing.T) {
	client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"Objects":[{"Id":1}]}`))
	}))

	if _, err := client.Assignments(t.Context(), 1); err == nil {
		t.Fatal("an object envelope was silently read as an empty list")
	}
}

func TestDeadlinesComeFromCoursesBeforeCalendar(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration) string { return FormatTime(now.Add(d)) }
	day := 24 * time.Hour

	routes := map[string]any{
		"/content/myItems/due/": map[string]any{"Objects": []any{
			map[string]any{"OrgUnitId": "7", "ItemId": 90, "ItemName": "Lab  4", "DueDate": at(2 * day), "DateCompleted": at(-day), "ItemUrl": "https://lms/lab4"},
			map[string]any{"OrgUnitId": "7", "ItemId": 91, "ItemName": "Reading", "DueDate": at(3 * day), "DateCompleted": nil},
		}, "Next": nil},
		"/7/grades/values/myGradeValues/": []any{
			map[string]any{"GradeObjectIdentifier": "500", "PointsNumerator": 9},
		},
		"/7/dropbox/folders/": []any{
			map[string]any{"Id": 1, "Name": "Lab 4", "DueDate": at(2 * day), "DisplayInCalendar": false},
			map[string]any{"Id": 2, "Name": "Essay", "DueDate": at(-2 * day), "Availability": map[string]any{"EndDate": at(day)}},
			map[string]any{"Id": 3, "Name": "Graded", "DueDate": at(day), "GradeItemId": 500},
			map[string]any{"Id": 4, "Name": "Old", "DueDate": at(-30 * day)},
			map[string]any{"Id": 5, "Name": "Hidden", "DueDate": at(day), "IsHidden": true},
			map[string]any{"Id": 6, "Name": "Next month", "DueDate": at(40 * day)},
		},
		"/7/dropbox/folders/2/submissions/mysubmissions/": []any{},
		"/7/dropbox/folders/3/submissions/mysubmissions/": []any{},
		"/7/quizzes/": map[string]any{"Objects": []any{
			map[string]any{"QuizId": 8, "Name": "Midterm", "DueDate": at(5 * day), "EndDate": at(5 * day), "IsActive": true},
		}},
		"/7/calendar/events/myEvents/": map[string]any{"Objects": []any{
			map[string]any{"CalendarEventId": 70, "Title": "Midterm due", "StartDateTime": at(5 * day),
				"AssociatedEntity": map[string]any{"AssociatedEntityType": "D2L.LE.Quizzing.Quiz", "AssociatedEntityId": 8}},
			map[string]any{"CalendarEventId": 71, "Title": "Guest lecture", "StartDateTime": at(4 * day), "EndDateTime": at(4 * day)},
		}, "Next": nil},
	}

	client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/d2l/api/le/1.80")
		body, ok := routes[path]
		if !ok {
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(body)
	}))

	report := client.Deadlines(t.Context(), []Course{{ID: 7, Name: "Databases"}}, now, 14)
	if report.Partial {
		t.Fatalf("unexpected partial report: %v", report.Warnings)
	}

	got := map[string]Deadline{}
	var order []string
	for _, d := range report.Items {
		got[d.Name] = d
		order = append(order, d.Name)
	}

	want := []string{"Essay", "Graded", "Lab 4", "Reading", "Guest lecture", "Midterm"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Fatalf("items = %v, want %v (sorted by date; hidden, stale and out-of-window items excluded)", order, want)
	}

	checks := []struct {
		name, kind, status string
		overdue, calendar  bool
	}{
		{"Essay", "assignment", StatusNotSubmitted, true, false},
		{"Graded", "assignment", StatusGraded, false, false},
		{"Lab 4", "assignment", StatusSubmitted, false, false},
		{"Reading", "content", StatusNotCompleted, false, false},
		{"Guest lecture", "calendar", StatusUnknown, false, true},
		{"Midterm", "quiz", StatusUnknown, false, true},
	}
	for _, check := range checks {
		d := got[check.name]
		if d.Kind != check.kind || d.Status != check.status || d.Overdue != check.overdue || d.OnCalendar != check.calendar {
			t.Errorf("%s = {kind %s status %s overdue %v calendar %v}, want {%s %s %v %v}",
				check.name, d.Kind, d.Status, d.Overdue, d.OnCalendar, check.kind, check.status, check.overdue, check.calendar)
		}
	}
	if got["Lab 4"].URL != "https://lms/lab4" {
		t.Errorf("Lab 4 URL = %q, want the linked content URL", got["Lab 4"].URL)
	}
}

func TestDeadlinesReportSourceFailures(t *testing.T) {
	client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/calendar/events/myEvents/") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/content/myItems/due/") || strings.HasSuffix(r.URL.Path, "/quizzes/") {
			_, _ = w.Write([]byte(`{"Objects":[],"Next":null}`))
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))

	report := client.Deadlines(t.Context(), []Course{{ID: 7, Name: "Databases"}}, time.Now(), 7)
	if !report.Partial || len(report.Warnings) != 1 || !strings.Contains(report.Warnings[0], "calendar") {
		t.Fatalf("partial = %v warnings = %v; want one calendar warning", report.Partial, report.Warnings)
	}
}
