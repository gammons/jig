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
// delimiters, but body keeps its original bytes. An opening delimiter with
// no closing delimiter is an error.
func Parse(src []byte, meta any) (string, error) {
	data := bytes.TrimPrefix(src, []byte(utf8BOM))

	firstNL := bytes.IndexByte(data, '\n')
	if firstNL < 0 || trimCR(data[:firstNL]) != delimiter {
		return string(src), nil
	}

	yamlStart := firstNL + 1
	pos := yamlStart
	for {
		next := bytes.IndexByte(data[pos:], '\n')
		if next < 0 {
			return "", fmt.Errorf("frontmatter: unterminated frontmatter block: no closing %q line", delimiter)
		}
		lineEnd := pos + next
		if trimCR(data[pos:lineEnd]) == delimiter {
			if err := unmarshalYAML(data[yamlStart:pos], meta); err != nil {
				return "", err
			}
			return string(data[lineEnd+1:]), nil
		}
		pos = lineEnd + 1
	}
}

// trimCR strips a trailing "\r" from b (present when the source line ended
// in "\r\n") and returns it as a string for delimiter comparison.
func trimCR(b []byte) string {
	return string(bytes.TrimSuffix(b, []byte("\r")))
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
