package canvas

import (
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/RobertLMcCrary/D2L-MCP/internal/lms"
)

// Error bodies are untrusted data. Extract only bounded diagnostic strings,
// never a whole response body, and redact the credential before returning them.
func (c *Client) responseError(response *http.Response) error {
	err := statusError(response.StatusCode)
	if err == nil {
		return nil
	}
	api := lms.AsError(err)
	data, readErr := io.ReadAll(io.LimitReader(response.Body, 8193))
	if readErr != nil || len(data) > 8192 {
		return api
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil {
		return api
	}
	var collect func(json.RawMessage, int, bool)
	collect = func(raw json.RawMessage, depth int, attributes bool) {
		if depth > 3 || len(api.Details) >= 8 {
			return
		}
		var message string
		if json.Unmarshal(raw, &message) == nil {
			message = strings.ReplaceAll(message, c.token, "[redacted]")
			message = strings.Map(func(r rune) rune {
				if r < 32 {
					return ' '
				}
				return r
			}, message)
			if len(message) > 1024 {
				message = string([]rune(message)[:min(len([]rune(message)), 256)])
			}
			if message = strings.TrimSpace(message); message != "" {
				api.Details = append(api.Details, message)
			}
			return
		}
		var messages []json.RawMessage
		if json.Unmarshal(raw, &messages) == nil {
			for _, m := range messages {
				collect(m, depth+1, attributes)
			}
			return
		}
		var object map[string]json.RawMessage
		if json.Unmarshal(raw, &object) == nil {
			keys := []string{"message", "error", "errors"}
			if attributes {
				keys = keys[:0]
				for key := range object {
					keys = append(keys, key)
				}
				sort.Strings(keys)
			}
			for _, key := range keys {
				if v, ok := object[key]; ok {
					collect(v, depth+1, attributes || key == "errors")
				}
			}
		}
	}
	found := false
	for _, key := range []string{"message", "error", "errors"} {
		if value, ok := fields[key]; ok {
			found = true
			collect(value, 0, key == "errors")
		}
	}
	// ActiveRecord validation failures may be bare attribute-to-array maps.
	// Do not treat arbitrary scalar fields as diagnostic messages.
	if !found {
		keys := make([]string, 0, len(fields))
		for key := range fields {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			raw := fields[key]
			if len(raw) > 0 && raw[0] == '[' {
				collect(raw, 0, true)
			}
		}
	}
	return api
}
