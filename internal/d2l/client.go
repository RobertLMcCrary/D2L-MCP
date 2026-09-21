package d2l

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/robertmccrary/d2l-mcp/internal/config"
)

const (
	maxRetries       = 3
	maxPages         = 200
	maxJSONBodyBytes = 32 << 20
)

type Object map[string]any

type Client struct {
	host       *url.URL
	http       *http.Client
	lpVersion  string
	leVersion  string
	maxRetries int
}

func NewClient(host, token string) (*Client, error) {
	u, err := url.Parse(host)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return nil, &Error{
			Code:    ErrConfig,
			Message: "A valid HTTPS Brightspace host is required.",
			Cause:   err,
		}
	}

	transport := &authTransport{
		base:      http.DefaultTransport,
		token:     token,
		origin:    strings.TrimRight(host, "/"),
		userAgent: config.UserAgent,
	}

	return &Client{
		host: u,
		http: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return http.ErrUseLastResponse
				}

				if !strings.EqualFold(req.URL.Hostname(), u.Hostname()) {
					return fmt.Errorf("refusing cross-host redirect to %s", req.URL.Hostname())
				}

				return nil
			},
		},
		lpVersion:  config.LPVersion,
		leVersion:  config.LEVersion,
		maxRetries: maxRetries,
	}, nil
}

func (c *Client) WithHTTPClient(client *http.Client) *Client {
	c.http = client
	return c
}

func (c *Client) LP(path string) string {
	return "/d2l/api/lp/" + c.lpVersion + path
}

func (c *Client) LE(path string) string {
	return "/d2l/api/le/" + c.leVersion + path
}

func (c *Client) Get(ctx context.Context, path string, query url.Values, out any) error {
	resp, err := c.request(ctx, path, query)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body := io.LimitReader(resp.Body, maxJSONBodyBytes+1)
	decoder := json.NewDecoder(body)

	if err := decoder.Decode(out); err != nil {
		return &Error{
			Code:    ErrInvalidResponse,
			Message: "Brightspace returned invalid JSON.",
			Cause:   err,
		}
	}

	return nil
}

func (c *Client) Download(ctx context.Context, path string) (*http.Response, error) {
	return c.request(ctx, path, nil)
}

func (c *Client) request(ctx context.Context, path string, query url.Values) (*http.Response, error) {
	if !strings.HasPrefix(path, "/d2l/api/") {
		return nil, &Error{Code: ErrConfig, Message: "Refusing a non-Valence API path."}
	}

	u := c.host.ResolveReference(&url.URL{Path: path})
	u.RawQuery = query.Encode()

	for attempt := 0; attempt < c.maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, err
		}

		resp, err := c.http.Do(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode != http.StatusTooManyRequests || attempt == c.maxRetries-1 {
			if err := responseError(resp); err != nil {
				resp.Body.Close()
				return nil, err
			}

			return resp, nil
		}

		resp.Body.Close()

		delay := retryDelay(resp.Header.Get("Retry-After"), attempt)
		timer := time.NewTimer(delay)

		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}

	panic("unreachable")
}

func responseError(resp *http.Response) error {
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return &Error{
			Code:       ErrAuth,
			Message:    "Brightspace authentication expired.",
			NextStep:   "Run: d2l-mcp login",
			StatusCode: resp.StatusCode,
		}
	case http.StatusForbidden:
		return &Error{
			Code:       ErrForbidden,
			Message:    "Brightspace denied access to this item.",
			StatusCode: resp.StatusCode,
		}
	case http.StatusNotFound:
		return &Error{
			Code:       ErrNotFound,
			Message:    "The requested Brightspace item was not found.",
			StatusCode: resp.StatusCode,
		}
	case http.StatusTooManyRequests:
		return &Error{
			Code:       ErrRateLimited,
			Message:    "Brightspace rate limit persisted after retries.",
			StatusCode: resp.StatusCode,
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &Error{
			Code:       ErrInvalidResponse,
			Message:    fmt.Sprintf("Brightspace returned HTTP %d.", resp.StatusCode),
			StatusCode: resp.StatusCode,
		}
	}

	return nil
}

func retryDelay(header string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(header); err == nil && seconds >= 0 {
		return min(time.Duration(seconds)*time.Second, 30*time.Second)
	}

	base := math.Pow(2, float64(attempt))

	return time.Duration(base*float64(time.Second)) + time.Duration(rand.IntN(250))*time.Millisecond
}

func (c *Client) paginateBookmark(ctx context.Context, path string, query url.Values) ([]Object, error) {
	if query == nil {
		query = url.Values{}
	}

	query = cloneValues(query)
	query.Set("bookmark", "")

	var result []Object
	seen := map[string]bool{}

	for page := 0; page < maxPages; page++ {
		var data struct {
			Items      []Object `json:"Items"`
			PagingInfo struct {
				HasMoreItems bool   `json:"HasMoreItems"`
				Bookmark     string `json:"Bookmark"`
			} `json:"PagingInfo"`
		}

		if err := c.Get(ctx, path, query, &data); err != nil {
			return nil, err
		}

		result = append(result, data.Items...)

		if !data.PagingInfo.HasMoreItems {
			return result, nil
		}

		if data.PagingInfo.Bookmark == "" || seen[data.PagingInfo.Bookmark] {
			return nil, &Error{
				Code:    ErrInvalidResponse,
				Message: "Brightspace returned an invalid pagination bookmark.",
			}
		}

		seen[data.PagingInfo.Bookmark] = true
		query.Set("bookmark", data.PagingInfo.Bookmark)
	}

	return nil, &Error{
		Code:    ErrInvalidResponse,
		Message: "Brightspace pagination exceeded the safety limit.",
	}
}

func (c *Client) paginatePages(ctx context.Context, path string) ([]Object, error) {
	var result []Object
	for page := 1; page <= maxPages; page++ {
		query := url.Values{"pageSize": {"50"}, "pageNumber": {strconv.Itoa(page)}}

		var batch []Object

		if err := c.Get(ctx, path, query, &batch); err != nil {
			return nil, err
		}

		result = append(result, batch...)

		if len(batch) < 50 {
			return result, nil
		}
	}

	return nil, &Error{
		Code:    ErrInvalidResponse,
		Message: "Brightspace pagination exceeded the safety limit.",
	}
}

func (c *Client) WhoAmI(ctx context.Context) (Object, error) {
	var out Object
	return out, c.Get(ctx, c.LP("/users/whoami"), nil, &out)
}

func (c *Client) Enrollments(ctx context.Context, activeOnly bool) ([]Object, error) {
	query := url.Values{"sortBy": {"-StartDate"}}
	if activeOnly {
		query.Set("isActive", "true")
		query.Set("canAccess", "true")
	}
	return c.paginateBookmark(ctx, c.LP("/enrollments/myenrollments/"), query)
}

func (c *Client) Grades(ctx context.Context, orgID int64) (any, error) {
	return c.getAny(ctx, c.LE(fmt.Sprintf("/%d/grades/values/myGradeValues/", orgID)), nil)
}

func (c *Client) FinalGrade(ctx context.Context, orgID int64) (any, error) {
	return c.getAny(ctx, c.LE(fmt.Sprintf("/%d/grades/final/values/myGradeValue", orgID)), nil)
}

func (c *Client) Assignments(ctx context.Context, orgID int64) ([]Object, error) {
	return c.getList(ctx, c.LE(fmt.Sprintf("/%d/dropbox/folders/", orgID)), nil, "")
}

func (c *Client) Quizzes(ctx context.Context, orgID int64) ([]Object, error) {
	return c.getList(ctx, c.LE(fmt.Sprintf("/%d/quizzes/", orgID)), nil, "Objects")
}

func (c *Client) Forums(ctx context.Context, orgID int64) ([]Object, error) {
	return c.getList(ctx, c.LE(fmt.Sprintf("/%d/discussions/forums/", orgID)), nil, "")
}

func (c *Client) Topics(ctx context.Context, orgID, forumID int64) ([]Object, error) {
	path := c.LE(fmt.Sprintf("/%d/discussions/forums/%d/topics/", orgID, forumID))

	return c.getList(ctx, path, nil, "")
}

func (c *Client) Posts(ctx context.Context, orgID, forumID, topicID int64) ([]Object, error) {
	path := c.LE(fmt.Sprintf(
		"/%d/discussions/forums/%d/topics/%d/posts/",
		orgID,
		forumID,
		topicID,
	))

	return c.paginatePages(ctx, path)
}

func (c *Client) News(ctx context.Context, orgID int64, since string) ([]Object, error) {
	query := url.Values{}
	if since != "" {
		query.Set("since", since)
	}

	return c.getList(ctx, c.LE(fmt.Sprintf("/%d/news/", orgID)), query, "")
}

func (c *Client) UserFeed(ctx context.Context, since string) ([]Object, error) {
	query := url.Values{}
	if since != "" {
		query.Set("since", since)
	}

	return c.getList(ctx, c.LP("/feed/"), query, "")
}

func (c *Client) Calendar(ctx context.Context, orgID *int64, start, end string) ([]Object, error) {
	query := url.Values{}
	if start != "" {
		query.Set("startDateTime", start)
	}
	if end != "" {
		query.Set("endDateTime", end)
	}

	path := c.LE("/calendar/events/myEvents/")

	if orgID != nil {
		path = c.LE(fmt.Sprintf("/%d/calendar/events/myEvents/", *orgID))
	}

	return c.getList(ctx, path, query, "")
}

func (c *Client) Due(ctx context.Context, orgIDs string, start, end string) ([]Object, error) {
	query := dateQuery(orgIDs, start, end)

	return c.getList(ctx, c.LE("/content/myItems/due/"), query, "Objects")
}

func (c *Client) Overdue(ctx context.Context, orgIDs string) ([]Object, error) {
	query := url.Values{}
	if orgIDs != "" {
		query.Set("orgUnitIdsCSV", orgIDs)
	}

	return c.getList(ctx, c.LE("/overdueItems/myItems"), query, "Objects")
}

func (c *Client) Updates(ctx context.Context, orgID *int64, orgIDs string) (any, error) {
	path := c.LE("/updates/myUpdates/")
	query := url.Values{}

	if orgID != nil {
		path = c.LE(fmt.Sprintf("/%d/updates/myUpdates", *orgID))
	} else if orgIDs != "" {
		query.Set("orgUnitIdsCSV", orgIDs)
	}

	return c.getAny(ctx, path, query)
}

func (c *Client) ContentRoot(ctx context.Context, orgID int64) ([]Object, error) {
	return c.getList(ctx, c.LE(fmt.Sprintf("/%d/content/root/", orgID)), nil, "")
}

func (c *Client) ContentModule(ctx context.Context, orgID, moduleID int64) ([]Object, error) {
	path := c.LE(fmt.Sprintf("/%d/content/modules/%d/structure/", orgID, moduleID))

	return c.getList(ctx, path, nil, "")
}

func (c *Client) ContentTOC(ctx context.Context, orgID int64) (any, error) {
	return c.getAny(ctx, c.LE(fmt.Sprintf("/%d/content/toc", orgID)), nil)
}

func (c *Client) TopicFile(ctx context.Context, orgID, topicID int64) (*http.Response, error) {
	return c.Download(ctx, c.LE(fmt.Sprintf("/%d/content/topics/%d/file", orgID, topicID)))
}

func (c *Client) AssignmentFile(ctx context.Context, orgID, folderID, fileID int64) (*http.Response, error) {
	path := c.LE(fmt.Sprintf(
		"/%d/dropbox/folders/%d/attachments/%d",
		orgID,
		folderID,
		fileID,
	))

	return c.Download(ctx, path)
}

func (c *Client) getAny(ctx context.Context, path string, query url.Values) (any, error) {
	var out any
	return out, c.Get(ctx, path, query, &out)
}

func (c *Client) getList(ctx context.Context, path string, query url.Values, objectKey string) ([]Object, error) {
	var out any
	if err := c.Get(ctx, path, query, &out); err != nil {
		return nil, err
	}

	if objectKey != "" {
		if object, ok := out.(map[string]any); ok {
			out = object[objectKey]
		}
	}

	raw, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}

	var items []Object

	if err := json.Unmarshal(raw, &items); err != nil {
		return []Object{}, nil
	}

	return items, nil
}

func dateQuery(orgIDs, start, end string) url.Values {
	query := url.Values{}
	if orgIDs != "" {
		query.Set("orgUnitIdsCSV", orgIDs)
	}
	if start != "" {
		query.Set("startDateTime", start)
	}
	if end != "" {
		query.Set("endDateTime", end)
	}

	return query
}

func cloneValues(values url.Values) url.Values {
	out := url.Values{}
	for key, entries := range values {
		out[key] = append([]string(nil), entries...)
	}

	return out
}

type authTransport struct {
	base      http.RoundTripper
	token     string
	origin    string
	userAgent string
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header = req.Header.Clone()

	clone.Header.Set("Authorization", "Bearer "+t.token)
	clone.Header.Set("Origin", t.origin)
	clone.Header.Set("Referer", t.origin+"/")
	clone.Header.Set("User-Agent", t.userAgent)
	clone.Header.Set("Accept", "application/json")

	return t.base.RoundTrip(clone)
}
