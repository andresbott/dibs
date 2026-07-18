package threewayrsync

import "time"

// FileState fingerprints one path: size and modification time for a regular file,
// presence only for a directory. There is no content hash — change detection uses
// rsync's own size+mtime quick-check.
type FileState struct {
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mtime"`
	IsDir   bool      `json:"isDir,omitempty"`
}

// Equal reports whether two states match. Files compare on size and mtime (rsync's
// quick check). Two directories are always equal: a dir's mtime churns with every
// content change inside it, so tracking it would turn every file edit into a phantom
// dir edit — presence is the only property a directory entry carries. A file on one
// side and a directory on the other never match (type change).
func (fs FileState) Equal(other FileState) bool {
	if fs.IsDir != other.IsDir {
		return false
	}
	if fs.IsDir {
		return true
	}
	return fs.Size == other.Size && fs.ModTime.Equal(other.ModTime)
}

// Manifest is a tree fingerprint keyed by slash-relative path.
type Manifest map[string]FileState

// countFiles counts the regular-file entries. The safety valves (wipe, empty-endpoint)
// judge a side by its files: a side holding only empty directories is still "wiped".
func countFiles(m Manifest) int {
	n := 0
	for _, st := range m {
		if !st.IsDir {
			n++
		}
	}
	return n
}
