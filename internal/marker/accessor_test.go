package marker

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/andresbott/dibs/libs/threewayrsync"
)

func TestForEndpointSelectsLocal(t *testing.T) {
	a := ForEndpoint(threewayrsync.Endpoint{Path: "/mnt/share"}, "")
	if _, ok := a.(localAccessor); !ok {
		t.Fatalf("accessor = %T, want localAccessor", a)
	}
	a = ForEndpoint(threewayrsync.Endpoint{Path: "/srv", SSH: &threewayrsync.SSH{Host: "h"}}, "")
	if _, ok := a.(*remoteAccessor); !ok {
		t.Fatalf("accessor = %T, want *remoteAccessor", a)
	}
}

func TestLocalAccessorRoundTrip(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	a := ForEndpoint(threewayrsync.Endpoint{Path: root}, "")

	if _, ok, err := a.Read(ctx); err != nil || ok {
		t.Fatalf("empty read: ok=%v err=%v", ok, err)
	}
	m := &Marker{CheckedOutBy: "me@host", Profile: "p", Host: "host", CheckedOutAt: time.Unix(100, 0).UTC()}
	if err := a.Write(ctx, m); err != nil {
		t.Fatal(err)
	}
	got, ok, err := a.Read(ctx)
	if err != nil || !ok || got.CheckedOutBy != "me@host" {
		t.Fatalf("read: %+v ok=%v err=%v", got, ok, err)
	}
	if err := a.Remove(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := a.Read(ctx); ok {
		t.Fatal("marker should be gone")
	}
}

// fakeTransport implements fileTransport over an in-memory map keyed by rel path.
type fakeTransport struct {
	files map[string][]byte
}

func (f *fakeTransport) FetchFile(_ context.Context, _ threewayrsync.Endpoint, rel, dstPath string) (bool, error) {
	data, ok := f.files[rel]
	if !ok {
		return false, nil
	}
	return true, os.WriteFile(dstPath, data, 0o600)
}

func (f *fakeTransport) PutFile(_ context.Context, _ threewayrsync.Endpoint, rel, srcPath string) error {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return err
	}
	f.files[rel] = data
	return nil
}

func (f *fakeTransport) DeleteFile(_ context.Context, _ threewayrsync.Endpoint, rel string) error {
	delete(f.files, rel)
	return nil
}

func TestRemoteAccessorRoundTrip(t *testing.T) {
	ctx := context.Background()
	ft := &fakeTransport{files: map[string][]byte{}}
	a := &remoteAccessor{endpoint: threewayrsync.Endpoint{Path: "/srv", SSH: &threewayrsync.SSH{Host: "h"}}, transport: ft}

	if _, ok, err := a.Read(ctx); err != nil || ok {
		t.Fatalf("empty read: ok=%v err=%v", ok, err)
	}
	m := &Marker{CheckedOutBy: "me@host", Profile: "p", Host: "host"}
	if err := a.Write(ctx, m); err != nil {
		t.Fatal(err)
	}
	// The uploaded bytes are valid marker JSON under the marker filename.
	var onWire Marker
	if err := json.Unmarshal(ft.files[FileName], &onWire); err != nil || onWire.Profile != "p" {
		t.Fatalf("wire content: %v %+v", err, onWire)
	}
	got, ok, err := a.Read(ctx)
	if err != nil || !ok || !got.OwnedBy("me@host", "host", "") {
		t.Fatalf("read: %+v ok=%v err=%v", got, ok, err)
	}
	if err := a.Remove(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := a.Read(ctx); ok {
		t.Fatal("marker should be gone after remove")
	}
}

func TestRemoteAccessorCorruptMarkerIsError(t *testing.T) {
	ft := &fakeTransport{files: map[string][]byte{FileName: []byte("not-json")}}
	a := &remoteAccessor{transport: ft}
	if _, _, err := a.Read(context.Background()); err == nil {
		t.Fatal("corrupt marker must error")
	}
}
