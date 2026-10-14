// Package canvas implements the Canvas REST protocol, independently of MCP.
package canvas

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/RobertLMcCrary/D2L-MCP/internal/lms"
	"github.com/RobertLMcCrary/D2L-MCP/internal/lmshttp"
	"github.com/tomnomnom/linkheader"
)

const maxJSON = 32 << 20
const maxPages = 200

type Client struct {
	origin *url.URL
	token  string
	http   *http.Client
	writes bool
}

var _ lms.Provider = (*Client)(nil)

// New requires an origin, not an API URL. Injecting a transport never bypasses
// authentication or redirect policy (and is useful for TLS fixture servers).
func New(host, token string, writes bool, transport http.RoundTripper) (*Client, error) {
	u, err := url.Parse(host)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, lms.Invalid("Canvas requires an HTTPS origin without credentials, path, query, or fragment")
	}
	if strings.TrimSpace(token) == "" || strings.ContainsAny(token, "\r\n") {
		return nil, &lms.Error{Code: "auth_required", Message: "Set CANVAS_TOKEN to a Canvas access token"}
	}
	if transport == nil {
		transport = http.DefaultTransport
	}
	c := &Client{origin: u, token: token, writes: writes}
	c.http = &http.Client{Transport: transport, Timeout: 45 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		// Redirects are handled explicitly so neither tokens nor request bodies
		// can escape the origin, including to another port on the same hostname.
		return http.ErrUseLastResponse
	}}
	return c, nil
}

func (c *Client) Capabilities() lms.Capabilities {
	return lms.Capabilities{Provider: "canvas", Read: true, Write: c.writes, Upload: c.writes, Download: true, APIPrefixes: []string{"/api/v1/", "/api/quiz/v1/"}}
}

func (c *Client) apiURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, lms.Invalid("Invalid Canvas API path")
	}
	if u.IsAbs() || u.Host != "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.Opaque != "" {
		return nil, lms.Invalid("Use a relative API path and the separate query parameter")
	}
	if err := validPath(u); err != nil {
		return nil, err
	}
	return c.origin.ResolveReference(u), nil
}

func validPath(u *url.URL) error {
	// Reject encoded traversal and separators as well as literal traversal.
	p := u.Path
	if path.Clean(p) != strings.TrimSuffix(p, "/") || strings.ContainsAny(p, "\\\x00\r\n") || strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") || strings.Contains(strings.ToLower(u.EscapedPath()), "%5c") || strings.Contains(p, "%") {
		return lms.Invalid("Canvas API path contains unsafe encoding or traversal")
	}
	if !strings.HasPrefix(p, "/api/v1/") && !strings.HasPrefix(p, "/api/quiz/v1/") {
		return lms.Invalid("Only Canvas REST API paths are supported")
	}
	return nil
}

func (c *Client) sameOrigin(u *url.URL) bool {
	return lmshttp.SameOrigin(u, c.origin)
}

func (c *Client) Do(ctx context.Context, r lms.Request) (lms.Response, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	method := strings.ToUpper(r.Method)
	if method == "" {
		method = http.MethodGet
	}
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE":
	default:
		return lms.Response{}, lms.Invalid("Unsupported HTTP method")
	}
	if method != "GET" && !c.writes {
		return lms.Response{}, &lms.Error{Code: "writes_disabled", Message: "Canvas writes are disabled", NextStep: "Run setup --provider canvas --enable-writes"}
	}
	if r.Paginate && method != "GET" {
		return lms.Response{}, lms.Invalid("Only GET requests can paginate")
	}
	if r.Collection != "" && (!r.Paginate || len(r.Collection) > 128) {
		return lms.Response{}, lms.Invalid("collection requires pagination and a top-level array field name up to 128 bytes")
	}
	if method == "GET" && len(r.Body) != 0 {
		return lms.Response{}, lms.Invalid("GET requests cannot have a body")
	}
	if len(r.Body) > maxJSON || (len(r.Body) != 0 && !json.Valid(r.Body)) {
		return lms.Response{}, lms.Invalid("Request body must be valid JSON under 32 MiB")
	}
	u, err := c.apiURL(r.Path)
	if err != nil {
		return lms.Response{}, err
	}
	q := lmshttp.CloneQuery(r.Query)
	for key := range q {
		if strings.EqualFold(key, "access_token") {
			return lms.Response{}, lms.Invalid("Access tokens must not appear in query parameters")
		}
	}
	if r.Paginate && q.Get("per_page") == "" {
		q.Set("per_page", "100")
	}
	if method == "GET" && strings.HasPrefix(u.Path, "/api/v1/conversations/") && len(strings.Split(strings.Trim(u.Path, "/"), "/")) == 4 {
		q.Set("auto_mark_as_read", "false")
	}
	u.RawQuery = q.Encode()
	seen := map[string]bool{}
	items := []json.RawMessage{}
	totalBytes := 0
	var envelope map[string]json.RawMessage
	metadata := []json.RawMessage{}
	for page := 1; page <= maxPages; page++ {
		if seen[u.String()] {
			return lms.Response{}, &lms.Error{Code: "invalid_response", Message: "Canvas pagination repeated a page"}
		}
		seen[u.String()] = true
		resp, err := c.request(ctx, method, u, r.Body)
		if err != nil {
			return lms.Response{}, err
		}
		data, readErr := readJSON(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return lms.Response{}, readErr
		}
		next, err := c.nextURL(resp.Header, u)
		if err != nil {
			return lms.Response{}, err
		}
		if !r.Paginate {
			result := lms.Response{Data: data, Pages: page}
			if next != nil && method == "GET" {
				result.Next = &lms.Request{Path: next.EscapedPath(), Query: next.Query(), Collection: r.Collection}
			}
			return result, nil
		}
		batchData := data
		if r.Collection != "" {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(data, &fields); err != nil || fields == nil {
				return lms.Response{}, &lms.Error{Code: "invalid_response", Message: "Canvas pagination requires an object envelope for the selected collection"}
			}
			batchData = bytes.TrimSpace(fields[r.Collection])
			if envelope == nil {
				envelope = map[string]json.RawMessage{}
				for key, value := range fields {
					envelope[key] = value
				}
			}
			delete(fields, r.Collection)
			raw, _ := json.Marshal(fields)
			metadata = append(metadata, raw)
		}
		var batch []json.RawMessage
		if err := json.Unmarshal(batchData, &batch); err != nil || len(batchData) == 0 || batchData[0] != '[' {
			return lms.Response{}, &lms.Error{Code: "invalid_response", Message: "Pagination requires an array; supply collection for an object envelope"}
		}
		items = append(items, batch...)
		totalBytes += len(data)
		if totalBytes > 2*maxJSON {
			return lms.Response{}, &lms.Error{Code: "limit_exceeded", Message: "Canvas pagination exceeds 64 MiB; narrow the query"}
		}
		if len(items) > 100000 {
			return lms.Response{}, &lms.Error{Code: "limit_exceeded", Message: "Canvas pagination exceeded 100000 items"}
		}
		if next == nil {
			encoded, _ := json.Marshal(items)
			if envelope != nil {
				envelope[r.Collection] = encoded
				encoded, _ = json.Marshal(envelope)
			}
			result := lms.Response{Data: encoded, Pages: page}
			if len(metadata) > 1 {
				result.PageMetadata = metadata
			}
			return result, nil
		}
		u = next
	}
	return lms.Response{}, &lms.Error{Code: "limit_exceeded", Message: "Canvas pagination exceeded 200 pages; narrow the query"}
}

func (c *Client) nextURL(headers http.Header, current *url.URL) (*url.URL, error) {
	next := ""
	for _, header := range headers.Values("Link") {
		for _, link := range linkheader.Parse(header) {
			for _, rel := range strings.Fields(link.Rel) {
				if rel == "next" {
					if next != "" {
						return nil, lms.Invalid("Canvas returned multiple next links")
					}
					next = link.URL
				}
			}
		}
	}
	if next == "" {
		return nil, nil
	}
	n, err := url.Parse(next)
	if err != nil {
		return nil, lms.Invalid("Invalid Canvas pagination URL")
	}
	n = current.ResolveReference(n)
	if n.String() == current.String() {
		return nil, &lms.Error{Code: "invalid_response", Message: "Canvas pagination repeated the current page"}
	}
	if !c.sameOrigin(n) || n.Fragment != "" || validPath(n) != nil {
		return nil, lms.Invalid("Unsafe Canvas pagination URL")
	}
	for key := range n.Query() {
		if strings.EqualFold(key, "access_token") {
			return nil, lms.Invalid("Access token in Canvas pagination URL")
		}
	}
	return safeReadURL(n)
}

func readJSON(r io.Reader) (json.RawMessage, error) {
	b, err := lmshttp.ReadJSON(r, maxJSON)
	if err != nil {
		return nil, &lms.Error{Code: "invalid_response", Message: "Canvas returned invalid or oversized JSON", Cause: err}
	}
	return b, nil
}

func (c *Client) request(ctx context.Context, method string, u *url.URL, body []byte) (*http.Response, error) {
	if !c.sameOrigin(u) {
		return nil, lms.Invalid("Refusing cross-origin Canvas request")
	}
	if method == http.MethodGet {
		var err error
		u, err = safeReadURL(u)
		if err != nil {
			return nil, err
		}
	}
	for attempt := 0; attempt < 4; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Accept", "application/json+canvas-string-ids")
		req.Header.Set("User-Agent", "lms-mcp/"+"dev")
		if len(body) != 0 {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := c.http.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			message := "Canvas request failed; check connectivity."
			if method != "GET" {
				message += " A write may have completed: inspect remote state before retrying."
			}
			return nil, &lms.Error{Code: "transport_error", Message: message, Cause: err}
		}
		// Never replay a mutation, even after a timeout or 429.
		remaining, budgetErr := strconv.ParseFloat(resp.Header.Get("X-Rate-Limit-Remaining"), 64)
		retry := method == "GET" && (resp.StatusCode == 429 || resp.StatusCode == 503 || (resp.StatusCode == 403 && budgetErr == nil && remaining <= 0))
		if retry && attempt < 3 {
			resp.Body.Close()
			delay := time.Duration(1<<attempt)*time.Second + time.Duration(rand.IntN(250))*time.Millisecond
			if s, err := strconv.ParseFloat(resp.Header.Get("Retry-After"), 64); err == nil && s >= 0 {
				delay = time.Duration(min(s, 30) * float64(time.Second))
			} else if t, err := http.ParseTime(resp.Header.Get("Retry-After")); err == nil {
				delay = max(0, min(30*time.Second, time.Until(t)))
			}
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
			continue
		}
		if err := c.responseError(resp); err != nil {
			resp.Body.Close()
			return nil, err
		}
		return resp, nil
	}
	panic("unreachable")
}

func statusError(status int) error {
	if status >= 200 && status < 300 {
		return nil
	}
	e := &lms.Error{Code: "api_error", StatusCode: status, Message: fmt.Sprintf("Canvas returned HTTP %d", status)}
	switch status {
	case 400, 422:
		e.Code = "invalid_input"
		e.NextStep = "Check Canvas field names, JSON nesting, accepted values and current item restrictions"
	case 401:
		e.Code = "auth_required"
		e.NextStep = "Replace CANVAS_TOKEN or run login --token-stdin"
	case 403:
		e.Code = "forbidden"
		e.NextStep = "Check token scopes and Canvas role permissions"
	case 404:
		e.Code = "not_found"
	case 429:
		e.Code = "rate_limited"
	case 301, 302, 303, 307, 308:
		e.Code = "redirect_refused"
		e.NextStep = "Configure the canonical Canvas host"
	}
	return e
}
