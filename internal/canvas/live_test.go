package canvas

import (
	"encoding/json"
	"os"
	"testing"
)

// Explicit opt-in; no uploads, submissions, messages or timed quiz starts.
func TestLiveStudentReadOnly(t *testing.T) {
	if os.Getenv("CANVAS_LIVE_TEST") != "1" {
		t.Skip("set CANVAS_LIVE_TEST=1, CANVAS_HOST and CANVAS_TOKEN for read-only school checks")
	}
	c, err := New(os.Getenv("CANVAS_HOST"), os.Getenv("CANVAS_TOKEN"), false, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"whoami", "get_settings", "get_planner", "list_conversations", "list_groups", "get_notifications"} {
		t.Run(name, func(t *testing.T) {
			if _, err := c.Read(t.Context(), name, nil, nil); err != nil {
				t.Fatal(err)
			}
		})
	}
	r, err := c.Read(t.Context(), "list_courses", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var courses []Course
	if err := json.Unmarshal(r.Data, &courses); err != nil {
		t.Fatal(err)
	}
	for _, course := range courses[:min(len(courses), 2)] {
		t.Run("course_"+string(course.ID), func(t *testing.T) {
			snapshot, err := c.Snapshot(t.Context(), string(course.ID))
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := snapshot.Data["get_course"]; !ok {
				t.Fatal("course details unavailable")
			}
			for name, warning := range snapshot.Warnings {
				t.Logf("optional surface %s unavailable: %s", name, warning)
			}
		})
	}
}
