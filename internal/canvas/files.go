package canvas

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/RobertLMcCrary/D2L-MCP/internal/lms"
)

const maxFileBytes int64 = 256 << 20

// Shared by upload routing and the documentation audit. Protocol tests assert
// independent literal routes, including the comment route's user-before-comments order.
var UploadRoutes = map[string]string{
	"user":       "/api/v1/users/self/files",
	"course":     "/api/v1/courses/{course_id}/files",
	"group":      "/api/v1/groups/{group_id}/files",
	"submission": "/api/v1/courses/{course_id}/assignments/{assignment_id}/submissions/self/files",
	"comment":    "/api/v1/courses/{course_id}/assignments/{assignment_id}/submissions/self/comments/files",
}

// externalHTTP has no bearer transport and rejects private DNS destinations.
// The resolved IP is pinned to the actual dial to prevent DNS rebinding.
func externalHTTP() *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = nil
	t.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		for _, ip := range ips {
			if !publicIP(ip) {
				return nil, fmt.Errorf("external file destination resolves to a non-public address")
			}
		}
		var last error
		for _, ip := range ips {
			conn, err := (&net.Dialer{Timeout: 15 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			last = err
		}
		if last == nil {
			last = fmt.Errorf("external file host has no addresses")
		}
		return nil, last
	}
	return &http.Client{Transport: t, Timeout: 5 * time.Minute, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
}

func publicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	// Shared address space and metadata service ranges are not public storage.
	for _, cidr := range []string{"100.64.0.0/10", "192.0.0.0/24", "198.18.0.0/15", "2001:db8::/32", "192.0.2.0/24", "198.51.100.0/24", "203.0.113.0/24"} {
		if netip.MustParsePrefix(cidr).Contains(ip) {
			return false
		}
	}
	return true
}

func fileURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
		return nil, lms.Invalid("Canvas file services must use HTTPS without credentials or fragments")
	}
	return u, nil
}

type UploadInput struct {
	Path         string `json:"path" jsonschema:"Relative file path within the managed download directory; only upload a file explicitly selected by the user"`
	CourseID     string `json:"course_id,omitempty"`
	AssignmentID string `json:"assignment_id,omitempty"`
	GroupID      string `json:"group_id,omitempty"`
	Comment      bool   `json:"comment,omitempty" jsonschema:"Upload for a submission comment instead of homework"`
}

func (c *Client) Upload(ctx context.Context, root string, input UploadInput) (lms.Response, error) {
	if !c.writes {
		return lms.Response{}, &lms.Error{Code: "writes_disabled", Message: "Enable Canvas writes before uploading"}
	}
	if input.Path == "" || filepath.IsAbs(input.Path) {
		return lms.Response{}, lms.Invalid("Use a relative file path inside the managed download directory")
	}
	managed, err := os.OpenRoot(root)
	if err != nil {
		return lms.Response{}, err
	}
	defer managed.Close()
	f, err := managed.Open(input.Path)
	if err != nil {
		return lms.Response{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return lms.Response{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxFileBytes {
		return lms.Response{}, lms.Invalid("Uploads must be regular files at most 256 MiB")
	}
	if input.GroupID != "" && (input.CourseID != "" || input.AssignmentID != "") {
		return lms.Response{}, lms.Invalid("Choose a group or course upload context")
	}
	if input.AssignmentID != "" && input.CourseID == "" || input.Comment && input.AssignmentID == "" {
		return lms.Response{}, lms.Invalid("Assignment/comment uploads require course_id and assignment_id")
	}
	op := Operation{Path: UploadRoutes["user"]}
	params := map[string]string{"course_id": input.CourseID, "assignment_id": input.AssignmentID, "group_id": input.GroupID}
	if input.GroupID != "" {
		op.Path = UploadRoutes["group"]
	} else if input.AssignmentID != "" {
		op.Path = UploadRoutes["submission"]
		if input.Comment {
			op.Path = UploadRoutes["comment"]
		}
	} else if input.CourseID != "" {
		op.Path = UploadRoutes["course"]
	}
	r, err := op.Request(params, nil)
	if err != nil {
		return lms.Response{}, err
	}
	r.Method = "POST"
	r.Body, _ = json.Marshal(map[string]any{"name": info.Name(), "size": info.Size(), "on_duplicate": "rename"})
	init, err := c.Do(ctx, r)
	if err != nil {
		return lms.Response{}, err
	}
	var ticket struct {
		URL    string                     `json:"upload_url"`
		Params map[string]json.RawMessage `json:"upload_params"`
	}
	if err := json.Unmarshal(init.Data, &ticket); err != nil {
		return lms.Response{}, err
	}
	u, err := fileURL(ticket.URL)
	if err != nil {
		return lms.Response{}, err
	}
	// Buffer only multipart framing. SectionReader streams the file with a known
	// Content-Length, including for S3 services that reject chunked uploads.
	var framing bytes.Buffer
	multi := multipart.NewWriter(&framing)
	keys := make([]string, 0, len(ticket.Params))
	for key := range ticket.Params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if key == "file" {
			return lms.Response{}, lms.Invalid("Upload ticket contains a reserved file parameter")
		}
		var value string
		if err := json.Unmarshal(ticket.Params[key], &value); err != nil {
			// Form-encoded scalar parameters are copied without float conversion.
			raw := bytes.TrimSpace(ticket.Params[key])
			if !json.Valid(raw) || len(raw) == 0 || raw[0] == '{' || raw[0] == '[' || bytes.Equal(raw, []byte("null")) {
				return lms.Response{}, lms.Invalid("Canvas upload parameters must be scalar values")
			}
			value = string(raw)
		}
		if err := multi.WriteField(key, value); err != nil {
			return lms.Response{}, err
		}
	}
	if _, err := multi.CreateFormFile("file", info.Name()); err != nil {
		return lms.Response{}, err
	}
	headerSize := framing.Len()
	if err := multi.Close(); err != nil {
		return lms.Response{}, err
	}
	reader := io.MultiReader(bytes.NewReader(framing.Bytes()[:headerSize]), io.NewSectionReader(f, 0, info.Size()), bytes.NewReader(framing.Bytes()[headerSize:]))
	req, err := http.NewRequestWithContext(ctx, "POST", u.String(), reader)
	if err != nil {
		return lms.Response{}, err
	}
	req.ContentLength = int64(framing.Len()) + info.Size()
	req.Header.Set("Content-Type", multi.FormDataContentType())
	// Even for same-origin storage, this step MUST omit the Canvas access token.
	storage := externalHTTP()
	if c.sameOrigin(u) {
		storage.Transport = c.http.Transport
	}
	resp, err := storage.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return lms.Response{}, ctx.Err()
		}
		return lms.Response{}, &lms.Error{Code: "upload_failed", Message: "File upload failed; inspect Canvas before retrying"}
	}
	defer resp.Body.Close()
	if location := resp.Header.Get("Location"); location != "" && (resp.StatusCode == 201 || resp.StatusCode >= 300 && resp.StatusCode < 400) {
		final, err := url.Parse(location)
		if err != nil {
			return lms.Response{}, lms.Invalid("Invalid upload completion URL")
		}
		final = u.ResolveReference(final)
		if !c.sameOrigin(final) || validPath(final) != nil || final.Fragment != "" || final.Query().Has("access_token") {
			return lms.Response{}, lms.Invalid("Unsafe upload completion URL")
		}
		response, err := c.request(ctx, "GET", final, nil)
		if err != nil {
			return lms.Response{}, err
		}
		defer response.Body.Close()
		data, err := readJSON(response.Body)
		if err != nil {
			return lms.Response{}, err
		}
		return completedUpload(data)
	}
	if err := statusError(resp.StatusCode); err != nil {
		return lms.Response{}, err
	}
	data, err := readJSON(resp.Body)
	if err != nil {
		return lms.Response{}, err
	}
	return completedUpload(data)
}

func completedUpload(data json.RawMessage) (lms.Response, error) {
	var file File
	if err := json.Unmarshal(data, &file); err != nil || file.ID == "" {
		return lms.Response{}, &lms.Error{Code: "invalid_response", Message: "Upload did not return a file ID"}
	}
	return lms.Response{Data: data, Pages: 1}, nil
}

type DownloadedFile struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	URI         string `json:"uri"`
	Size        int64  `json:"size"`
	FileID      ID     `json:"file_id"`
	ContentType string `json:"content_type,omitempty"`
}

func (f DownloadedFile) Artifact() lms.FileArtifact {
	return lms.FileArtifact{Name: f.Name, URI: f.URI, Size: f.Size, ContentType: f.ContentType}
}

func (c *Client) DownloadFile(ctx context.Context, root, fileID string) (DownloadedFile, error) {
	r, err := c.Read(ctx, "get_file", map[string]string{"file_id": fileID}, nil)
	if err != nil {
		return DownloadedFile{}, err
	}
	file, err := lms.Decode[File](r.Data)
	if err != nil {
		return DownloadedFile{}, err
	}
	meta := file.Value
	if meta.LockedForUser || meta.HiddenForUser {
		return DownloadedFile{}, &lms.Error{Code: "forbidden", Message: "Canvas file is locked or hidden for this user"}
	}
	if meta.Size < 0 || meta.Size > maxFileBytes {
		return DownloadedFile{}, lms.Invalid("File exceeds 256 MiB")
	}
	u, err := fileURL(meta.URL)
	if err != nil {
		return DownloadedFile{}, err
	}
	var resp *http.Response
	for redirects := 0; redirects <= 5; redirects++ {
		// Signed URLs omit credentials. Unsigned URLs receive a bearer token only
		// on the configured Canvas origin, never on a redirected external service.
		client := externalHTTP()
		if c.sameOrigin(u) {
			client.Transport = c.http.Transport
		}
		req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
		if err != nil {
			return DownloadedFile{}, err
		}
		if c.sameOrigin(u) && u.Query().Get("verifier") == "" {
			req.Header.Set("Authorization", "Bearer "+c.token)
		}
		resp, err = client.Do(req)
		if err != nil {
			return DownloadedFile{}, &lms.Error{Code: "download_failed", Message: "Canvas file download failed"}
		}
		if resp.StatusCode < 300 || resp.StatusCode >= 400 {
			break
		}
		location := resp.Header.Get("Location")
		resp.Body.Close()
		next, err := url.Parse(location)
		if err != nil || location == "" {
			return DownloadedFile{}, lms.Invalid("Invalid download redirect")
		}
		next = u.ResolveReference(next)
		u, err = fileURL(next.String())
		if err != nil {
			return DownloadedFile{}, err
		}
		if redirects == 5 {
			return DownloadedFile{}, lms.Invalid("Too many download redirects")
		}
	}
	defer resp.Body.Close()
	if err := statusError(resp.StatusCode); err != nil {
		return DownloadedFile{}, err
	}
	if resp.ContentLength > maxFileBytes {
		return DownloadedFile{}, lms.Invalid("File exceeds 256 MiB")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return DownloadedFile{}, err
	}
	managed, err := os.OpenRoot(root)
	if err != nil {
		return DownloadedFile{}, err
	}
	defer managed.Close()
	name := safeName(meta.DisplayName)
	if name == "" {
		name = safeName("file-" + fileID)
	}
	// os.Root protects against symlink escapes and concurrent path replacement.
	var f *os.File
	for suffix := 0; suffix < 1000; suffix++ {
		candidate := name
		if suffix > 0 {
			candidate = strings.TrimSuffix(name, filepath.Ext(name)) + "-" + strconv.Itoa(suffix) + filepath.Ext(name)
		}
		f, err = managed.OpenFile(candidate, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return DownloadedFile{}, err
		}
		name = candidate
		break
	}
	if f == nil {
		return DownloadedFile{}, lms.Invalid("Too many filename collisions")
	}
	complete := false
	defer func() {
		f.Close()
		if !complete {
			managed.Remove(name)
		}
	}()
	n, err := io.Copy(f, io.LimitReader(resp.Body, maxFileBytes+1))
	if err != nil {
		return DownloadedFile{}, err
	}
	if n > maxFileBytes {
		return DownloadedFile{}, lms.Invalid("File exceeds 256 MiB")
	}
	if err := f.Sync(); err != nil {
		return DownloadedFile{}, err
	}
	if err := f.Close(); err != nil {
		return DownloadedFile{}, err
	}
	complete = true
	abs, err := filepath.Abs(filepath.Join(root, name))
	if err != nil {
		return DownloadedFile{}, err
	}
	contentType := meta.ContentType
	if contentType == "" {
		contentType = resp.Header.Get("Content-Type")
	}
	return DownloadedFile{Name: name, Path: abs, URI: (&url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}).String(), Size: n, FileID: meta.ID, ContentType: contentType}, nil
}

func safeName(raw string) string {
	raw = strings.ReplaceAll(raw, "\\", "/")
	raw = filepath.Base(raw)
	raw = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"|?*`, r) {
			return '_'
		}
		return r
	}, raw)
	raw = strings.Trim(raw, " .")
	if raw == "." || raw == ".." || raw == "" {
		return ""
	}
	if len(raw) > 180 {
		raw = string([]rune(raw)[:min(len([]rune(raw)), 60)])
	}
	stem := strings.ToUpper(strings.SplitN(raw, ".", 2)[0])
	switch stem {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		raw = "_" + raw
	}
	return raw
}
