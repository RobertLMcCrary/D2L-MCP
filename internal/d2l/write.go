package d2l

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RobertLMcCrary/D2L-MCP/internal/lms"
	"github.com/RobertLMcCrary/D2L-MCP/internal/lmshttp"
)

const (
	// Brightspace accepts roughly 488 MB per request; stay well under it.
	maxUploadBytes int64 = 256 << 20
	maxUploadFiles       = 20
	writeTimeout         = 10 * time.Minute
)

// WithWrites enables mutation requests. Without it every write is refused
// in the client, not just hidden from the tool list.
func (c *Client) WithWrites(enabled bool) *Client {
	c.writes = enabled
	return c
}

func writesDisabled() error {
	return &Error{
		Code:     "writes_disabled",
		Message:  "Brightspace writes are disabled.",
		NextStep: "Run: d2l-mcp setup --enable-writes",
	}
}

// Write sends one JSON mutation and decodes the JSON reply into out when
// out is non-nil. A nil body sends an empty request with Content-Length: 0.
func (c *Client) Write(ctx context.Context, method, path string, body, out any) error {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return err
		}
	}

	return c.send(ctx, method, path, "application/json", bytes.NewReader(payload), int64(len(payload)), out)
}

// Part is one multipart/mixed section. Brightspace requires the JSON part
// first and file parts after it.
type Part struct {
	JSON any
	File *UploadFile
}

type UploadFile struct {
	// FieldName is the Content-Disposition name. Dropbox submissions use "",
	// news and discussion attachments use "file 0", "file 1", ...
	FieldName   string
	Name        string
	ContentType string
	Size        int64
	Reader      io.Reader
}

// WriteMultipart streams a multipart/mixed body with an exact
// Content-Length, which Brightspace requires; files are never buffered.
func (c *Client) WriteMultipart(ctx context.Context, method, path string, parts []Part, out any) error {
	boundary, err := newBoundary()
	if err != nil {
		return err
	}

	var readers []io.Reader
	var length int64
	frame := func(text string) {
		readers = append(readers, strings.NewReader(text))
		length += int64(len(text))
	}

	for i, part := range parts {
		if i > 0 {
			frame("\r\n")
		}
		frame("--" + boundary + "\r\n")

		switch {
		case part.File != nil:
			f := part.File
			frame(fmt.Sprintf("Content-Disposition: form-data; name=%q; filename=%q\r\nContent-Type: %s\r\n\r\n",
				f.FieldName, f.Name, f.ContentType))
			readers = append(readers, io.LimitReader(f.Reader, f.Size))
			length += f.Size
		default:
			data, err := json.Marshal(part.JSON)
			if err != nil {
				return err
			}
			frame("Content-Type: application/json\r\n\r\n" + string(data))
		}
	}
	frame("\r\n--" + boundary + "--\r\n")

	contentType := "multipart/mixed; boundary=" + boundary

	return c.send(ctx, method, path, contentType, io.MultiReader(readers...), length, out)
}

// send performs a single mutation. It never retries: a repeated POST could
// submit twice. Redirects are refused rather than followed with the body.
func (c *Client) send(ctx context.Context, method, path, contentType string, body io.Reader, length int64, out any) error {
	if !c.writes {
		return writesDisabled()
	}
	u, err := c.apiURL(path, nil)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return err
	}
	req.ContentLength = length
	if length == 0 {
		req.Body = http.NoBody
	}
	req.Header.Set("Content-Type", contentType)

	client := *c.http
	client.Timeout = writeTimeout
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &Error{
			Code:     "write_failed",
			Message:  "The Brightspace write did not complete: " + err.Error(),
			NextStep: "Inspect Brightspace before retrying; the change may have been applied.",
			Cause:    err,
		}
	}
	defer resp.Body.Close()

	if err := writeError(resp); err != nil {
		return err
	}
	if out == nil {
		return nil
	}

	data, err := lmshttp.ReadJSON(resp.Body, maxJSONBodyBytes)
	if err != nil {
		return &Error{Code: ErrInvalidResponse, Message: "Brightspace returned invalid JSON.", Cause: err}
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}

	return json.Unmarshal(data, out)
}

// writeError keeps Brightspace's own explanation for rejected writes, which
// is usually the only clue to why a submission or post was refused.
func writeError(resp *http.Response) error {
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return &Error{
			Code:       ErrInvalidResponse,
			Message:    fmt.Sprintf("Brightspace redirected a write (HTTP %d); it was not followed.", resp.StatusCode),
			StatusCode: resp.StatusCode,
		}
	}

	err := responseError(resp)
	if err == nil {
		return nil
	}

	apiErr := AsError(err)
	if apiErr.Code == ErrRateLimited {
		apiErr.Message = "Brightspace rate-limited the write; it was not retried."
	}
	if detail := readDetail(resp.Body); detail != "" {
		apiErr.Details = append(apiErr.Details, detail)
	}

	return apiErr
}

func readDetail(body io.Reader) string {
	data, _ := io.ReadAll(io.LimitReader(body, 2048))
	text := strings.TrimSpace(string(data))
	if strings.HasPrefix(text, "<") {
		return ""
	}

	return text
}

func newBoundary() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}

	return "d2lmcp" + hex.EncodeToString(raw[:]), nil
}

// OpenUploads opens user-selected files relative to the managed download
// root. os.Root rejects absolute paths, traversal and symlink escapes. On
// success the caller must call the returned close function.
func OpenUploads(root string, paths []string) ([]*UploadFile, func(), error) {
	if len(paths) > maxUploadFiles {
		return nil, nil, lms.Invalid(fmt.Sprintf("Upload at most %d files at once", maxUploadFiles))
	}

	managed, err := os.OpenRoot(root)
	if err != nil {
		return nil, nil, &Error{Code: ErrUnsafePath, Message: "The managed download directory is not available.", Cause: err}
	}
	defer managed.Close()

	var opened []*os.File
	closeAll := func() {
		for _, f := range opened {
			_ = f.Close()
		}
	}

	uploads := make([]*UploadFile, 0, len(paths))
	var total int64

	for _, path := range paths {
		upload, f, err := openUpload(managed, path)
		if f != nil {
			opened = append(opened, f)
		}
		if err != nil {
			closeAll()
			return nil, nil, err
		}

		total += upload.Size
		if total > maxUploadBytes {
			closeAll()
			return nil, nil, &Error{Code: ErrTooLarge, Message: "Uploads must total at most 256 MiB."}
		}
		uploads = append(uploads, upload)
	}

	return uploads, closeAll, nil
}

func openUpload(managed *os.Root, path string) (*UploadFile, *os.File, error) {
	if path == "" || filepath.IsAbs(path) {
		return nil, nil, &Error{Code: ErrUnsafePath, Message: "Use a relative path inside the managed download directory."}
	}

	f, err := managed.Open(path)
	if err != nil {
		return nil, nil, &Error{Code: ErrUnsafePath, Message: "Cannot open " + path + " inside the managed download directory.", Cause: err}
	}

	info, err := f.Stat()
	if err != nil {
		return nil, f, err
	}
	if !info.Mode().IsRegular() {
		return nil, f, &Error{Code: ErrUnsafePath, Message: path + " is not a regular file."}
	}

	contentType := mime.TypeByExtension(filepath.Ext(info.Name()))
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	return &UploadFile{
		Name:        safeHeaderValue(info.Name()),
		ContentType: contentType,
		Size:        info.Size(),
		Reader:      f,
	}, f, nil
}

// safeHeaderValue keeps a filename from breaking out of its MIME header.
func safeHeaderValue(name string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || r == 127 || r == '"' || r == '\\' {
			return '_'
		}
		return r
	}, name)
}
