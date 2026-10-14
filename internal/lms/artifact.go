package lms

// FileArtifact is the common projection used by MCP clients to expose managed
// downloads. Providers keep their richer download metadata in structured output.
type FileArtifact struct {
	Name        string
	URI         string
	Size        int64
	ContentType string
}
