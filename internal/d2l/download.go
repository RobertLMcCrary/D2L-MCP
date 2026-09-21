package d2l

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const maxDownloadBytes int64 = 256 << 20

type DownloadedFile struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	URI         string `json:"uri"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type,omitempty"`
	SourceID    int64  `json:"source_id"`
}

type Downloader struct {
	client *Client
	root   string
}

func NewDownloader(client *Client, root string) *Downloader {
	return &Downloader{client: client, root: root}
}

func (d *Downloader) Assignment(ctx context.Context, course Course, query string) ([]DownloadedFile, error) {
	assignments, err := d.client.Assignments(ctx, course.ID)
	if err != nil {
		return nil, err
	}

	assignment, err := matchNamedObject(assignments, query, "Id", "Name", "assignment")
	if err != nil {
		return nil, err
	}

	folderID, _ := numberInt64(assignment["Id"])
	attachments, _ := assignment["Attachments"].([]any)

	dir := filepath.Join(
		d.root,
		courseDir(course),
		"assignments",
		safeFilename(stringValue(assignment["Name"]), fmt.Sprintf("assignment-%d", folderID)),
	)

	var files []DownloadedFile

	for _, raw := range attachments {
		attachment, _ := raw.(map[string]any)
		fileID, _ := numberInt64(attachment["FileId"])
		name := safeFilename(stringValue(attachment["FileName"]), fmt.Sprintf("file-%d", fileID))

		resp, err := d.client.AssignmentFile(ctx, course.ID, folderID, fileID)
		if err != nil {
			return nil, err
		}

		file, err := d.save(resp, dir, name, fileID)
		if err != nil {
			return nil, err
		}

		files = append(files, file)
	}

	return files, nil
}

func (d *Downloader) Topic(
	ctx context.Context,
	course Course,
	topicID int64,
	fallbackName string,
) (DownloadedFile, error) {
	resp, err := d.client.TopicFile(ctx, course.ID, topicID)
	if err != nil {
		return DownloadedFile{}, err
	}

	dir := filepath.Join(d.root, courseDir(course), "materials")
	name := safeFilename(fallbackName, fmt.Sprintf("topic-%d", topicID))

	return d.save(resp, dir, name, topicID)
}

func (d *Downloader) Module(ctx context.Context, course Course, query string) ([]DownloadedFile, error) {
	// Module matching and recursive TopicType==1 traversal are compatible with:
	// https://github.com/Aaryan-Kapoor/d2l-cli/blob/v0.2.2/src/d2l/commands/download.py
	root, err := d.client.ContentRoot(ctx, course.ID)
	if err != nil {
		return nil, err
	}

	modules, err := d.findModules(ctx, course.ID, root, strings.ToLower(strings.TrimSpace(query)))
	if err != nil {
		return nil, err
	}

	module, err := chooseModule(modules, query)
	if err != nil {
		return nil, err
	}

	moduleID := objectID(module)

	topics, err := d.collectTopics(ctx, course.ID, moduleID, map[int64]bool{})
	if err != nil {
		return nil, err
	}

	dir := filepath.Join(
		d.root,
		courseDir(course),
		"materials",
		safeFilename(objectTitle(module), fmt.Sprintf("module-%d", moduleID)),
	)

	var files []DownloadedFile

	for _, topic := range topics {
		topicID := objectID(topic)

		resp, err := d.client.TopicFile(ctx, course.ID, topicID)
		if err != nil {
			if apiErr := AsError(err); apiErr.Code == ErrForbidden || apiErr.Code == ErrNotFound {
				continue
			}

			return nil, err
		}

		name := safeFilename(objectTitle(topic), fmt.Sprintf("topic-%d", topicID))

		file, err := d.save(resp, dir, name, topicID)
		if err != nil {
			return nil, err
		}

		files = append(files, file)
	}

	return files, nil
}

func (d *Downloader) findModules(
	ctx context.Context,
	orgID int64,
	items []Object,
	query string,
) ([]Object, error) {
	var matches []Object
	var walk func([]Object, int) error

	walk = func(nodes []Object, depth int) error {
		if depth > 20 {
			return &Error{
				Code:    ErrInvalidResponse,
				Message: "Content module nesting exceeded the safety limit.",
			}
		}

		for _, node := range nodes {
			if intValue(node["Type"]) != 0 {
				continue
			}

			titleMatches := strings.Contains(strings.ToLower(objectTitle(node)), query)
			idMatches := strconv.FormatInt(objectID(node), 10) == query

			if titleMatches || idMatches {
				matches = append(matches, node)
			}

			children := objectSlice(node["Modules"])

			if len(children) == 0 && objectID(node) != 0 {
				fetched, err := d.client.ContentModule(ctx, orgID, objectID(node))
				if err != nil {
					apiErr := AsError(err)

					if apiErr.Code == ErrForbidden || apiErr.Code == ErrNotFound {
						continue
					}

					return err
				}

				children = fetched
			}

			if err := walk(children, depth+1); err != nil {
				return err
			}
		}

		return nil
	}

	return matches, walk(items, 0)
}

func (d *Downloader) collectTopics(
	ctx context.Context,
	orgID int64,
	moduleID int64,
	seen map[int64]bool,
) ([]Object, error) {
	if seen[moduleID] {
		return nil, nil
	}

	seen[moduleID] = true

	children, err := d.client.ContentModule(ctx, orgID, moduleID)
	if err != nil {
		return nil, err
	}

	var topics []Object

	for _, child := range children {
		switch intValue(child["Type"]) {
		case 1:
			if intValue(child["TopicType"]) == 1 {
				topics = append(topics, child)
			}
		case 0:
			nested, err := d.collectTopics(ctx, orgID, objectID(child), seen)
			if err != nil {
				return nil, err
			}

			topics = append(topics, nested...)
		}
	}

	return topics, nil
}

func (d *Downloader) save(
	resp *http.Response,
	dir string,
	fallback string,
	sourceID int64,
) (DownloadedFile, error) {
	defer resp.Body.Close()

	if err := secureMkdirAll(d.root, dir); err != nil {
		return DownloadedFile{}, err
	}

	name := fallback

	if _, params, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition")); err == nil {
		if candidate := params["filename"]; candidate != "" {
			name = safeFilename(candidate, fallback)
		}
	}

	dest, err := secureDestination(d.root, dir, name)
	if err != nil {
		return DownloadedFile{}, err
	}

	file, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return DownloadedFile{}, &Error{
				Code:    ErrUnsafePath,
				Message: fmt.Sprintf("Refusing to overwrite %s.", dest),
			}
		}

		return DownloadedFile{}, err
	}

	success := false

	defer func() {
		file.Close()

		if !success {
			os.Remove(dest)
		}
	}()

	written, err := io.Copy(file, io.LimitReader(resp.Body, maxDownloadBytes+1))
	if err != nil {
		return DownloadedFile{}, err
	}

	if written > maxDownloadBytes {
		return DownloadedFile{}, &Error{
			Code:    ErrTooLarge,
			Message: "Download exceeded the 256 MiB safety limit.",
		}
	}

	if err := file.Sync(); err != nil {
		return DownloadedFile{}, err
	}

	success = true

	absolute, _ := filepath.Abs(dest)

	return DownloadedFile{
		Name:        name,
		Path:        absolute,
		URI:         "file://" + filepath.ToSlash(absolute),
		Size:        written,
		ContentType: resp.Header.Get("Content-Type"),
		SourceID:    sourceID,
	}, nil
}

func secureMkdirAll(root, dir string) error {
	rootAbs, _ := filepath.Abs(root)
	dirAbs, _ := filepath.Abs(dir)

	if dirAbs != rootAbs && !strings.HasPrefix(dirAbs, rootAbs+string(os.PathSeparator)) {
		return &Error{Code: ErrUnsafePath, Message: "Download destination escapes the managed root."}
	}

	if err := os.MkdirAll(dirAbs, 0o700); err != nil {
		return err
	}

	if info, err := os.Lstat(rootAbs); err != nil {
		return err
	} else if info.Mode()&os.ModeSymlink != 0 {
		return &Error{Code: ErrUnsafePath, Message: "Managed download root cannot be a symbolic link."}
	}

	current := rootAbs
	relative, _ := filepath.Rel(rootAbs, dirAbs)

	for _, part := range strings.Split(relative, string(os.PathSeparator)) {
		if part == "." || part == "" {
			continue
		}

		current = filepath.Join(current, part)

		info, err := os.Lstat(current)
		if err != nil {
			return err
		}

		if info.Mode()&os.ModeSymlink != 0 {
			return &Error{Code: ErrUnsafePath, Message: "Download path contains a symbolic link."}
		}
	}

	return nil
}

func secureDestination(root, dir, name string) (string, error) {
	if name != filepath.Base(name) {
		return "", &Error{Code: ErrUnsafePath, Message: "Unsafe download filename."}
	}

	dest := filepath.Join(dir, name)
	rootAbs, _ := filepath.Abs(root)
	destAbs, _ := filepath.Abs(dest)

	if !strings.HasPrefix(destAbs, rootAbs+string(os.PathSeparator)) {
		return "", &Error{Code: ErrUnsafePath, Message: "Download destination escapes the managed root."}
	}

	return destAbs, nil
}

func safeFilename(name, fallback string) string {
	name = filepath.Base(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"))
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return fallback
	}

	var cleaned strings.Builder

	for _, r := range name {
		if r < 32 || strings.ContainsRune(`<>:"|?*`, r) {
			cleaned.WriteRune('_')
		} else {
			cleaned.WriteRune(r)
		}
	}

	return cleaned.String()
}

func courseDir(course Course) string {
	return fmt.Sprintf("%s-%d", safeFilename(course.Name, "course"), course.ID)
}

func matchNamedObject(items []Object, query, idKey, nameKey, kind string) (Object, error) {
	query = strings.TrimSpace(query)
	for _, item := range items {
		if stringValue(item[idKey]) == query || strings.EqualFold(stringValue(item[nameKey]), query) {
			return item, nil
		}
	}

	var matches []Object

	for _, item := range items {
		if strings.Contains(strings.ToLower(stringValue(item[nameKey])), strings.ToLower(query)) {
			matches = append(matches, item)
		}
	}

	if len(matches) == 1 {
		return matches[0], nil
	}

	if len(matches) > 1 {
		return nil, &Error{
			Code:     ErrAmbiguous,
			Message:  fmt.Sprintf("Multiple %ss match %q.", kind, query),
			NextStep: "Use the numeric ID.",
		}
	}

	return nil, &Error{Code: ErrNotFound, Message: fmt.Sprintf("No %s matching %q.", kind, query)}
}

func chooseModule(items []Object, query string) (Object, error) {
	if len(items) == 0 {
		return nil, &Error{Code: ErrNotFound, Message: fmt.Sprintf("No content module matching %q.", query)}
	}

	if len(items) == 1 {
		return items[0], nil
	}

	var exact []Object

	for _, item := range items {
		if strings.EqualFold(objectTitle(item), query) || strconv.FormatInt(objectID(item), 10) == query {
			exact = append(exact, item)
		}
	}

	if len(exact) == 1 {
		return exact[0], nil
	}

	return nil, &Error{
		Code:     ErrAmbiguous,
		Message:  fmt.Sprintf("Multiple content modules match %q.", query),
		NextStep: "Use the numeric module ID.",
	}
}

func objectTitle(object Object) string {
	if title := stringValue(object["Title"]); title != "" {
		return title
	}

	return stringValue(object["Name"])
}

func objectID(object Object) int64 {
	if id, ok := numberInt64(object["Id"]); ok {
		return id
	}

	id, _ := numberInt64(object["ModuleId"])

	if id == 0 {
		id, _ = numberInt64(object["TopicId"])
	}

	return id
}

func intValue(value any) int {
	id, _ := numberInt64(value)
	return int(id)
}

func objectSlice(value any) []Object {
	raw, ok := value.([]any)
	if !ok {
		return nil
	}

	out := make([]Object, 0, len(raw))

	for _, item := range raw {
		if object, ok := item.(map[string]any); ok {
			out = append(out, object)
		}
	}

	return out
}
