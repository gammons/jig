// Package frontmatter parses "---\n<yaml>\n---\n<body>" documents: markdown
// files with a leading YAML metadata block, as used by skills and agents.
package frontmatter

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"
)

// utf8BOM is the UTF-8 byte order mark that may precede the opening
// delimiter.
const utf8BOM = "\xef\xbb\xbf"

const delimiter = "---"

// Parse splits src into a YAML frontmatter block and a body. If src opens
// with a "---" line (after an optional UTF-8 BOM), Parse unmarshals the
// YAML between it and the matching closing "---" line into meta and returns
// everything after the closing line's newline as body. If src does not
// open with "---", meta is left untouched and body is src unchanged. CRLF
// line endings are supported: they are treated like "\n" when locating the
// delimiters, but body keeps its original bytes. A delimiter line may carry
// trailing whitespace (spaces or tabs) before its line ending. A closing
// delimiter line may also be the last line of src with no trailing newline
// at all, in which case body is "". An opening delimiter with no matching
// closing delimiter anywhere in src is an error.
//
// Note: a line that is exactly "---" (ignoring trailing whitespace) inside
// a YAML block scalar in the frontmatter body would be mistaken for the
// closing delimiter; Parse does not track YAML block-scalar nesting.
func Parse(src []byte, meta any) (string, error) {
	data := bytes.TrimPrefix(src, []byte(utf8BOM))

	firstNL := bytes.IndexByte(data, '\n')
	if firstNL < 0 || !isDelimiterLine(data[:firstNL]) {
		return string(src), nil
	}

	yamlStart := firstNL + 1
	pos := yamlStart
	for {
		next := bytes.IndexByte(data[pos:], '\n')
		if next < 0 {
			// No more newlines: the rest of src is the last line. If it is
			// itself a closing delimiter, it terminates the block with an
			// empty body; otherwise the block was never closed.
			if isDelimiterLine(data[pos:]) {
				if err := unmarshalYAML(data[yamlStart:pos], meta); err != nil {
					return "", err
				}
				return "", nil
			}
			return "", fmt.Errorf("frontmatter: unterminated frontmatter block: no closing %q line", delimiter)
		}
		lineEnd := pos + next
		if isDelimiterLine(data[pos:lineEnd]) {
			if err := unmarshalYAML(data[yamlStart:pos], meta); err != nil {
				return "", err
			}
			return string(data[lineEnd+1:]), nil
		}
		pos = lineEnd + 1
	}
}

// isDelimiterLine reports whether b is a "---" delimiter line: b, with a
// trailing "\r" (from a CRLF line ending) and any trailing spaces/tabs
// removed, equals exactly "---".
func isDelimiterLine(b []byte) bool {
	return string(bytes.TrimRight(b, " \t\r")) == delimiter
}

// unmarshalYAML decodes b into meta, treating an all-whitespace block (the
// empty-frontmatter case) as a no-op that leaves meta untouched. CRLF line
// endings within the block are normalized to "\n" before decoding.
func unmarshalYAML(b []byte, meta any) error {
	if len(bytes.TrimSpace(b)) == 0 {
		return nil
	}
	normalized := bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
	if err := yaml.Unmarshal(normalized, meta); err != nil {
		return fmt.Errorf("frontmatter: parsing yaml: %w", err)
	}
	return nil
}
