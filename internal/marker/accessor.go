package marker

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/candy-tools/dibs/libs/threewayrsync"
)

// Accessor reads, writes, and removes the per-profile marker on a remote root reached
// through any endpoint kind: direct file operations on a mounted path, single-file rsync
// transfers for an ssh or daemon endpoint.
type Accessor interface {
	Read(ctx context.Context) (*Marker, bool, error)
	Write(ctx context.Context, m *Marker) error
	Remove(ctx context.Context) error
}

// fileTransport is the single-file rsync surface the remote accessor needs;
// *threewayrsync.Syncer satisfies it. Injectable for tests.
type fileTransport interface {
	FetchFile(ctx context.Context, e threewayrsync.Endpoint, rel, dstPath string) (bool, error)
	PutFile(ctx context.Context, e threewayrsync.Endpoint, rel, srcPath string) error
	DeleteFile(ctx context.Context, e threewayrsync.Endpoint, rel string) error
}

// ForEndpoint returns the marker accessor for a remote root endpoint: direct filesystem
// access when the endpoint is a local path (mounted share), rsync single-file transfers
// otherwise. bin overrides the rsync binary for the transfers; empty means "rsync" from
// PATH.
func ForEndpoint(e threewayrsync.Endpoint, bin string) Accessor {
	if e.SSH == nil && e.Daemon == nil {
		return localAccessor{root: e.Path}
	}
	// The Syncer's Store is unused by the single-file helpers, so a store-less Syncer is
	// enough here.
	return &remoteAccessor{endpoint: e, transport: &threewayrsync.Syncer{Bin: bin}}
}

// localAccessor wraps the direct file operations for a mounted remote root.
type localAccessor struct{ root string }

func (a localAccessor) Read(context.Context) (*Marker, bool, error) { return Read(a.root) }
func (a localAccessor) Write(_ context.Context, m *Marker) error    { return Write(a.root, m) }
func (a localAccessor) Remove(context.Context) error                { return Remove(a.root) }

// remoteAccessor moves the marker as a single file over rsync (ssh or daemon transport).
type remoteAccessor struct {
	endpoint  threewayrsync.Endpoint
	transport fileTransport
}

func (a *remoteAccessor) Read(ctx context.Context) (*Marker, bool, error) {
	tmp, err := os.MkdirTemp("", "dibs-marker-*")
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	dst := filepath.Join(tmp, FileName)
	found, err := a.transport.FetchFile(ctx, a.endpoint, FileName, dst)
	if err != nil || !found {
		return nil, false, err
	}
	data, err := os.ReadFile(dst) //nolint:gosec // G304: reading the temp file this function just fetched into its own temp dir.
	if err != nil {
		return nil, false, err
	}
	var m Marker
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, false, err
	}
	return &m, true, nil
}

func (a *remoteAccessor) Write(ctx context.Context, m *Marker) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "dibs-marker-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	src := filepath.Join(tmp, FileName)
	// 0644 mirrors the mounted-path marker: a shared, cross-user cooperative lock must be
	// readable by other users and machines. PutFile carries the mode with --perms.
	if err := os.WriteFile(src, data, 0o644); err != nil { //nolint:gosec // G306: intentionally world-readable, see comment.
		return err
	}
	return a.transport.PutFile(ctx, a.endpoint, FileName, src)
}

func (a *remoteAccessor) Remove(ctx context.Context) error {
	return a.transport.DeleteFile(ctx, a.endpoint, FileName)
}
