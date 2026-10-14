// canvas-contract audits named operation routes against current official docs.
// This verifies published HTTP methods/paths, not school-specific permissions or
// live behavior. No Canvas credentials or institution data are used.
package main

import (
	"context"
	"fmt"
	"github.com/RobertLMcCrary/D2L-MCP/internal/canvas"
	"golang.org/x/sync/errgroup"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

var route = regexp.MustCompile("(?m)#### `([A-Z]+) (/api/[^`]+)`")
var param = regexp.MustCompile(`^:[a-zA-Z_]+$`)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 20 * time.Second}
	group, gctx := errgroup.WithContext(ctx)
	group.SetLimit(4)
	var mu sync.Mutex
	routes := map[string][]string{}
	for _, resource := range []string{"users", "courses", "assignments", "assignment_groups", "submissions", "submission_comments", "enrollments", "modules", "pages", "files", "discussion_topics", "conversations", "calendar_events", "planner", "quizzes", "quiz_submissions", "quiz_submission_questions", "new_quizzes", "groups", "tabs", "rubrics", "communication_channels", "notification_preferences", "favorites", "peer_reviews", "collaborations", "conferences", "appointment_groups", "progress", "e_portfolios", "outcome_results"} {
		group.Go(func() error {
			req, err := http.NewRequestWithContext(gctx, "GET", "https://developerdocs.instructure.com/services/canvas/resources/"+resource+".md", nil)
			if err != nil {
				return err
			}
			resp, err := client.Do(req)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				return fmt.Errorf("%s: HTTP %d", resource, resp.StatusCode)
			}
			body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
			if err != nil {
				return err
			}
			mu.Lock()
			defer mu.Unlock()
			for _, m := range route.FindAllStringSubmatch(string(body), -1) {
				routes[m[1]] = append(routes[m[1]], m[2])
			}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return err
	}
	missing := []string{}
	checked := 0
	check := func(name, method, path string) {
		checked++
		for _, published := range routes[method] {
			if matches(path, published) {
				return
			}
		}
		missing = append(missing, name+" "+method+" "+path)
	}
	for _, op := range canvas.Operations {
		check(op.Name, "GET", op.Path)
	}
	for _, op := range canvas.Mutations {
		check(op.Name, op.Method, op.Path)
	}
	for name, path := range canvas.UploadRoutes {
		check("upload_"+name, "POST", path)
	}
	check("submit_assignment", "POST", "/api/v1/courses/{course_id}/assignments/{assignment_id}/submissions")
	// DELETE /done is documented in prose under PUT /done.
	filtered := missing[:0]
	for _, m := range missing {
		if strings.HasPrefix(m, "uncomplete_module_item ") {
			path := strings.TrimPrefix(m, "uncomplete_module_item DELETE ")
			verified := false
			for _, published := range routes["PUT"] {
				if matches(path, published) {
					verified = true
					break
				}
			}
			if verified {
				continue
			}
		}
		filtered = append(filtered, m)
	}
	missing = filtered
	if len(missing) > 0 {
		return fmt.Errorf("%d/%d routes not found in documentation headings:\n%s", len(missing), checked, strings.Join(missing, "\n"))
	}
	fmt.Printf("Verified %d Canvas routes including uploads and submission against current official documentation. Live permissions and payload semantics require separate tests.\n", checked)
	return nil
}
func matches(actual, published string) bool {
	a := strings.Split(strings.Trim(actual, "/"), "/")
	p := strings.Split(strings.Trim(published, "/"), "/")
	if len(a) != len(p) {
		return false
	}
	for i := range a {
		if param.MatchString(p[i]) && a[i] != "" {
			continue
		}
		if a[i] != p[i] {
			return false
		}
	}
	return true
}
