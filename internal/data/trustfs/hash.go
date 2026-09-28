package trustfs

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
)

// maxHashBytes caps how much of one file Hash reads; a bigger file is
// hashed by its size alone.
const maxHashBytes = 1 << 20

// Hash returns a sha256 hex digest over files, sorted by path, so the
// result doesn't depend on the caller's order. Each file contributes
// strconv.Itoa(len(path)) + "\n" + path + strconv.Itoa(len(content)) +
// "\n" + content. Both the path and the content are length-prefixed:
// project filenames are attacker-controlled (a repo can contain a
// filename with an embedded newline and digits), so prefixing only the
// content's length would let a crafted path forge the boundary between
// one file's record and the next, making two different file sets hash
// equal. Length-prefixing the path too fixes exactly how many bytes are
// path before the content-length digits appear, closing that gap.
// Hash(nil) is "", not the hash of an empty stream, so "no files" is
// distinguishable from "one empty file".
func Hash(files []string) (string, error) {
	return HashOptional(files, nil)
}

// HashOptional is Hash over files plus optional, files that may not exist.
// Besides a present file's record, a file contributes one of these, none
// of which can collide with a present file's (whose content length starts
// with a digit), strconv.Itoa(len(path)) + "\n" + path followed by:
//
//   - "-\n": a missing optional file;
//   - "!unreadable\n": a non-regular file (device, FIFO, directory), which
//     is never read, or an optional file that cannot be read (EACCES,
//     ENOTDIR, ...);
//   - "!size " + size + "\n": a regular file over maxHashBytes, which is
//     hashed by its size alone.
//
// A missing or unreadable regular file in files is still an error. The
// result is "" only when both lists are empty.
func HashOptional(files, optional []string) (string, error) {
	if len(files) == 0 && len(optional) == 0 {
		return "", nil
	}
	type entry struct {
		path     string
		optional bool
	}
	all := make([]entry, 0, len(files)+len(optional))
	for _, f := range files {
		all = append(all, entry{f, false})
	}
	for _, f := range optional {
		all = append(all, entry{f, true})
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].path < all[j].path })

	h := sha256.New()
	for _, e := range all {
		rec, err := record(e.path, e.optional)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s\n%s", strconv.Itoa(len(e.path)), e.path)
		h.Write(rec)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// record is path's part of the hash after its length-prefixed path (see
// HashOptional).
func record(path string, optional bool) ([]byte, error) {
	info, err := os.Stat(path)
	switch {
	case optional && os.IsNotExist(err):
		return []byte("-\n"), nil
	case err == nil && !info.Mode().IsRegular():
		return []byte("!unreadable\n"), nil
	case err == nil && info.Size() > maxHashBytes:
		return fmt.Appendf(nil, "!size %d\n", info.Size()), nil
	}
	var content []byte
	if err == nil {
		content, err = readCapped(path)
	}
	if err != nil {
		if optional {
			return []byte("!unreadable\n"), nil
		}
		return nil, fmt.Errorf("trustfs: reading %s: %w", path, err)
	}
	if len(content) > maxHashBytes { // grew since the Stat
		return fmt.Appendf(nil, "!size %d\n", len(content)), nil
	}
	return append(fmt.Appendf(nil, "%d\n", len(content)), content...), nil
}

// readCapped reads at most maxHashBytes+1 bytes of path.
func readCapped(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, maxHashBytes+1))
}
