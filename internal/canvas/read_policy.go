package canvas

import (
	"github.com/RobertLMcCrary/D2L-MCP/internal/lms"
	"net/url"
	"strings"
)

// Canvas has GET includes that change read state. Apply this to every actual
// request (including pagination), regardless of the account's write setting.
func safeReadURL(input *url.URL) (*url.URL, error) {
	query := input.Query()
	for key, values := range query {
		key = strings.ToLower(key)
		if key == "include" || strings.HasPrefix(key, "include[") {
			for _, value := range values {
				if value == "read_state" || value == "read_status" {
					return nil, &lms.Error{Code: "read_only_violation", Message: "Canvas read_state/read_status includes mark submission feedback as read", NextStep: "Use a separately requested write operation to change read state"}
				}
			}
		}
	}
	if strings.HasPrefix(input.Path, "/api/v1/conversations/") && len(strings.Split(strings.Trim(input.Path, "/"), "/")) == 4 && query.Get("auto_mark_as_read") != "false" {
		copy := *input
		query.Set("auto_mark_as_read", "false")
		copy.RawQuery = query.Encode()
		return &copy, nil
	}
	return input, nil
}
