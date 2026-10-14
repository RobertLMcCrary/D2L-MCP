package lmshttp

import (
	"net/url"
	"strings"
)

// SameOrigin is deliberately conservative: an alternate port or protocol is
// a different destination, even when the hostname happens to be identical.
func SameOrigin(a, b *url.URL) bool {
	return a != nil && b != nil && a.Scheme == b.Scheme && strings.EqualFold(a.Host, b.Host) && a.User == nil && b.User == nil
}
