package core

// Media is binary content, such as an image, attached to a Part or a
// ToolResult. Data is never persisted: the store keeps only MIME and Ref,
// and client/llm fills Data from the blob store just before it builds a
// single LLM request.
type Media struct {
	MIME string
	Ref  string // blob sha256 (64 lowercase hex)
	Data []byte `json:"-"`
}

// Attachment is a file the user (or a tool, e.g. a screenshot) attached to
// a message: text content read inline, or image Media backed by a blob.
type Attachment struct {
	Path    string // absolute path as resolved by chat
	Content string // text attachments
	Media   *Media // image attachments
}

// ImageInfo describes a decoded image's dimensions and encoded size, as
// reported by the read tool's "image WxH (<bytes>)" output.
type ImageInfo struct {
	Width, Height, Bytes int
}
