package d2l

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/RobertLMcCrary/D2L-MCP/internal/lms"
)

var _ lms.Provider = (*Client)(nil)

func (c *Client) Capabilities() lms.Capabilities {
	return lms.Capabilities{
		Provider:    "d2l",
		Read:        true,
		Write:       c.writes,
		Upload:      c.writes,
		Download:    true,
		APIPrefixes: []string{"/d2l/api/"},
	}
}

func (c *Client) Do(ctx context.Context, r lms.Request) (lms.Response, error) {
	if r.Method != "" && r.Method != http.MethodGet {
		if r.Paginate || r.Collection != "" {
			return lms.Response{}, lms.Invalid("Pagination applies only to reads")
		}
		if len(r.Query) != 0 {
			return lms.Response{}, lms.Invalid("Put write parameters in the JSON body")
		}
		var body any
		if len(r.Body) != 0 {
			body = r.Body
		}
		var data json.RawMessage
		err := c.Write(ctx, r.Method, r.Path, body, &data)
		return lms.Response{Data: data, Pages: 1}, err
	}
	if len(r.Body) != 0 || r.Paginate || r.Collection != "" {
		return lms.Response{}, lms.Unsupported("generic request bodies or automatic pagination; use named D2L tools")
	}
	var data json.RawMessage
	err := c.Get(ctx, r.Path, r.Query, &data)
	return lms.Response{Data: data, Pages: 1}, err
}
