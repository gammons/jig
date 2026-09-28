package chat

import (
	"io/fs"
	"path/filepath"

	"github.com/gammons/jig/internal/core"
	"github.com/gammons/jig/internal/service/media"
	"github.com/gammons/jig/internal/service/tools"
)

// maxAttachmentBytes bounds a non-image (text) attachment's file size.
const maxAttachmentBytes = 50 * 1024

// maxAttachmentImageBytes bounds an image attachment's file size, before
// Images.Process scales and re-encodes it.
const maxAttachmentImageBytes = 20 << 20

// FileReader is the file-system capability Send needs to read and stat
// attachments. tools.OSFS() satisfies it.
type FileReader interface {
	Stat(string) (fs.FileInfo, error)
	ReadFile(string) ([]byte, error)
}

// ReadMarker records that a session has read a file, so a later edit or
// write to it needs no prior read tool call. The shared *tools.Tracker
// (the same instance the read/write/edit tools use) satisfies it.
type ReadMarker interface {
	MarkRead(sid core.SessionID, path string, info fs.FileInfo)
}

// Imager turns raw image bytes into a bounded, stored core.Media.
// *media.Pipeline satisfies it.
type Imager interface {
	Process(data []byte) (core.Media, core.ImageInfo, error)
}

// textRead is a text attachment's absolute path and the fs.FileInfo Send
// marks as read once the session exists.
type textRead struct {
	path string
	info fs.FileInfo
}

// resolveAttachments validates and loads each of paths (relative ones
// resolve against s.d.WorkDir), returning one core.Attachment per path, in
// order, and the text attachments' FileInfo to mark as read once the
// session exists. Every failure is a *ConfigError, and nothing further is
// validated.
func (s *Service) resolveAttachments(paths []string) ([]core.Attachment, []textRead, error) {
	if len(paths) == 0 {
		return nil, nil, nil
	}
	atts := make([]core.Attachment, 0, len(paths))
	var reads []textRead
	for _, p := range paths {
		abs := resolveAttachmentPath(s.d.WorkDir, p)
		info, err := s.d.Files.Stat(abs)
		if err != nil {
			return nil, nil, configErr("attachment %s: no such file", abs)
		}
		if info.IsDir() {
			return nil, nil, configErr("attachment %s is a directory", abs)
		}
		if media.IsImagePath(abs) {
			att, err := s.imageAttachment(abs, info)
			if err != nil {
				return nil, nil, err
			}
			atts = append(atts, att)
			continue
		}
		att, err := s.textAttachment(abs, info)
		if err != nil {
			return nil, nil, err
		}
		atts = append(atts, att)
		reads = append(reads, textRead{path: abs, info: info})
	}
	return atts, reads, nil
}

// imageAttachment reads and decodes abs through Images.Process, returning
// an Attachment holding the resulting Media (Ref set, Data not yet
// loaded).
func (s *Service) imageAttachment(abs string, info fs.FileInfo) (core.Attachment, error) {
	if info.Size() > maxAttachmentImageBytes {
		return core.Attachment{}, configErr("attachment %s is larger than 20 MB", abs)
	}
	data, err := s.d.Files.ReadFile(abs)
	if err != nil {
		return core.Attachment{}, configErr("attachment %s: %v", abs, err)
	}
	m, _, err := s.d.Images.Process(data)
	if err != nil {
		return core.Attachment{}, configErr("attachment %s: %v", abs, err)
	}
	return core.Attachment{Path: abs, Media: &m}, nil
}

// textAttachment reads abs, refusing it if it is larger than
// maxAttachmentBytes or looks binary.
func (s *Service) textAttachment(abs string, info fs.FileInfo) (core.Attachment, error) {
	if info.Size() > maxAttachmentBytes {
		return core.Attachment{}, configErr("attachment %s is larger than 50 KB", abs)
	}
	data, err := s.d.Files.ReadFile(abs)
	if err != nil {
		return core.Attachment{}, configErr("attachment %s: %v", abs, err)
	}
	if tools.LooksBinary(data) {
		return core.Attachment{}, configErr("attachment %s is binary", abs)
	}
	return core.Attachment{Path: abs, Content: string(data)}, nil
}

// resolveAttachmentPath resolves p against workDir when it is relative,
// and cleans the result.
func resolveAttachmentPath(workDir, p string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Clean(filepath.Join(workDir, p))
}
