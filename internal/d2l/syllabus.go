package d2l

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"
)

var (
	termPattern       = regexp.MustCompile(`(?i)\b(Fall|Spring|Summer) Semester \d{4}\b`)
	courseCodePattern = regexp.MustCompile(`^([A-Za-z]+)(\d+[A-Za-z]?)$`)
	slugSeparators    = regexp.MustCompile(`[ _.\-/]+`)

	blockTags = map[string]bool{
		"address":    true,
		"article":    true,
		"aside":      true,
		"blockquote": true,
		"br":         true,
		"div":        true,
		"dl":         true,
		"dt":         true,
		"dd":         true,
		"figcaption": true,
		"figure":     true,
		"footer":     true,
		"h1":         true,
		"h2":         true,
		"h3":         true,
		"h4":         true,
		"h5":         true,
		"h6":         true,
		"header":     true,
		"hr":         true,
		"li":         true,
		"main":       true,
		"nav":        true,
		"ol":         true,
		"p":          true,
		"pre":        true,
		"section":    true,
		"table":      true,
		"td":         true,
		"th":         true,
		"tr":         true,
		"ul":         true,
	}

	ignoredTags = map[string]bool{
		"script":   true,
		"style":    true,
		"noscript": true,
		"template": true,
	}
)

type Syllabus struct {
	Course     string         `json:"course"`
	CRN        string         `json:"crn"`
	ID         any            `json:"syllabus_id"`
	URL        string         `json:"url"`
	Title      string         `json:"title,omitempty"`
	Subtitle   string         `json:"sub_title,omitempty"`
	Term       string         `json:"term,omitempty"`
	Modified   any            `json:"modified,omitempty"`
	Instructor map[string]any `json:"instructor,omitempty"`
	Text       string         `json:"text"`
}

type SyllabusProvider struct {
	host string
	http *http.Client
}

func NewSyllabusProvider(host string) *SyllabusProvider {
	return &SyllabusProvider{
		host: strings.TrimRight(host, "/"),
		http: &http.Client{Timeout: 20 * time.Second},
	}
}

func (p *SyllabusProvider) Fetch(ctx context.Context, course Course) (Syllabus, error) {
	if p.host == "" {
		return Syllabus{}, &Error{
			Code:     ErrConfig,
			Message:  "No SimpleSyllabus host is configured.",
			NextStep: "Run: d2l-mcp setup --syllabus-host https://school.simplesyllabus.com",
		}
	}

	crn := extractCRN(course.Code)
	if crn == "" {
		return Syllabus{}, &Error{
			Code:    ErrNotFound,
			Message: fmt.Sprintf("Could not extract a CRN from course code %q.", course.Code),
		}
	}

	item, err := p.find(ctx, course, crn)
	if err != nil {
		return Syllabus{}, err
	}

	path := documentPath(item)

	body, err := p.get(ctx, p.host+"/api2/doc-html/"+path)
	if err != nil {
		return Syllabus{}, err
	}
	defer body.Close()

	text, err := readableHTML(io.LimitReader(body, 16<<20))
	if err != nil {
		return Syllabus{}, &Error{
			Code:    ErrInvalidResponse,
			Message: "Could not parse the syllabus document.",
			Cause:   err,
		}
	}

	instructor, _ := item["instructor"].(map[string]any)

	return Syllabus{
		Course:     course.Name,
		CRN:        crn,
		ID:         item["syllabus_id"],
		URL:        p.host + "/en-US/doc/" + path + "?mode=view",
		Title:      stringValue(item["title"]),
		Subtitle:   stringValue(item["sub_title"]),
		Term:       stringValue(item["term_name"]),
		Modified:   item["last_updated"],
		Instructor: instructor,
		Text:       text,
	}, nil
}

func (p *SyllabusProvider) find(ctx context.Context, course Course, crn string) (map[string]any, error) {
	// Candidate scoring preserves the upstream SimpleSyllabus behavior:
	// https://github.com/Aaryan-Kapoor/d2l-cli/blob/v0.2.2/src/d2l/commands/syllabus.py
	endpoint := p.host + "/api2/syllabus-search?search=" + url.QueryEscape(crn)
	body, err := p.get(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	defer body.Close()

	var result struct {
		Items []map[string]any `json:"items"`
	}

	if err := json.NewDecoder(io.LimitReader(body, 8<<20)).Decode(&result); err != nil {
		return nil, &Error{
			Code:    ErrInvalidResponse,
			Message: "SimpleSyllabus returned invalid JSON.",
			Cause:   err,
		}
	}

	term, subject, number := courseIdentity(course)
	bestScore := -1

	var best map[string]any

	for _, item := range result.Items {
		if !strings.Contains(stringValue(item["title"]), crn) || item["syllabus_id"] == nil {
			continue
		}

		score := 0

		if term != "" && strings.EqualFold(stringValue(item["term_name"]), term) {
			score += 100
		}
		if subject != "" && strings.EqualFold(stringValue(item["subject_name"]), subject) {
			score += 25
		}
		if number != "" && strings.EqualFold(stringValue(item["course_number"]), number) {
			score += 25
		}

		subtitle := strings.ToLower(stringValue(item["sub_title"]))
		if subtitle != "" && strings.Contains(strings.ToLower(course.Name), subtitle) {
			score += 10
		}

		if score > bestScore {
			best, bestScore = item, score
		}
	}

	if best == nil {
		return nil, &Error{
			Code:    ErrNotFound,
			Message: fmt.Sprintf("No syllabus found for CRN %s.", crn),
		}
	}

	return best, nil
}

func (p *SyllabusProvider) get(ctx context.Context, endpoint string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	resp, err := p.http.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()

		return nil, &Error{
			Code:       ErrNotFound,
			Message:    fmt.Sprintf("SimpleSyllabus returned HTTP %d.", resp.StatusCode),
			StatusCode: resp.StatusCode,
		}
	}

	return resp.Body, nil
}

func extractCRN(code string) string {
	parts := strings.Split(code, ".")
	if len(parts) >= 4 {
		return parts[3]
	}

	return ""
}

func courseIdentity(course Course) (term, subject, number string) {
	term = termPattern.FindString(course.Name)
	parts := strings.Split(course.Code, ".")

	if len(parts) >= 3 {
		match := courseCodePattern.FindStringSubmatch(parts[2])

		if len(match) == 3 {
			subject, number = match[1], match[2]
		}
	}

	return
}

func documentPath(item map[string]any) string {
	display := strings.TrimSpace(stringValue(item["term_name"]) + " " + stringValue(item["title"]))
	if subtitle := stringValue(item["sub_title"]); subtitle != "" {
		display += " - " + subtitle
	}

	slug := slugSeparators.ReplaceAllString(display, "-")

	return url.PathEscape(stringValue(item["syllabus_id"])) + "/" + url.PathEscape(slug)
}

func readableHTML(reader io.Reader) (string, error) {
	root, err := html.Parse(reader)
	if err != nil {
		return "", err
	}

	var parts []string
	var walk func(*html.Node, bool)

	walk = func(node *html.Node, skip bool) {
		if node.Type == html.ElementNode && ignoredTags[node.Data] {
			skip = true
		}

		if skip {
			return
		}

		if node.Type == html.ElementNode && blockTags[node.Data] {
			parts = append(parts, "\n")
		}

		if node.Type == html.TextNode {
			parts = append(parts, node.Data)
		}

		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child, skip)
		}

		if node.Type == html.ElementNode && blockTags[node.Data] {
			parts = append(parts, "\n")
		}
	}

	walk(root, false)

	var lines []string

	for _, raw := range strings.Split(strings.Join(parts, ""), "\n") {
		line := strings.Join(strings.Fields(raw), " ")

		if line != "" && (len(lines) == 0 || lines[len(lines)-1] != line) {
			lines = append(lines, line)
		}
	}

	return strings.Join(lines, "\n"), nil
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}
