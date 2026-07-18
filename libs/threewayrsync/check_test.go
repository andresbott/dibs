package threewayrsync

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

const gnuRsyncVersionOut = `rsync  version 3.2.7  protocol version 31
Copyright (C) 1996-2022 by Andrew Tridgell, Wayne Davison, and others.
Web site: https://rsync.samba.org/
`

const openRsyncVersionOut = `openrsync: protocol version 29
rsync version 2.6.9 compatible
`

func TestParseRsyncVersion(t *testing.T) {
	tests := []struct {
		name        string
		out         string
		wantVersion string
		wantOpen    bool
		wantErr     string // substring; "" means no error
	}{
		{name: "gnu 3.2.7", out: gnuRsyncVersionOut, wantVersion: "3.2.7"},
		{name: "gnu 3.1.0", out: "rsync  version 3.1.0  protocol version 31\n", wantVersion: "3.1.0"},
		{name: "openrsync", out: openRsyncVersionOut, wantOpen: true, wantErr: "openrsync"},
		{name: "too old", out: "rsync  version 2.6.9  protocol version 29\n", wantVersion: "2.6.9", wantErr: "too old"},
		{name: "3.0 below minor", out: "rsync  version 3.0.9  protocol version 30\n", wantVersion: "3.0.9", wantErr: "too old"},
		{name: "garbage", out: "not an rsync at all\n", wantErr: "could not parse"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := parseRsyncVersion(tt.out)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want substring %q", err, tt.wantErr)
				}
			}
			if info.Version != tt.wantVersion {
				t.Errorf("Version = %q, want %q", info.Version, tt.wantVersion)
			}
			if info.OpenRsync != tt.wantOpen {
				t.Errorf("OpenRsync = %v, want %v", info.OpenRsync, tt.wantOpen)
			}
		})
	}
}

func TestCheckBinaryUsesConfiguredBin(t *testing.T) {
	var gotBin string
	var gotArgs []string
	run := func(_ context.Context, bin string, args []string, _ io.Writer) (runResult, error) {
		gotBin, gotArgs = bin, args
		return runResult{stdout: gnuRsyncVersionOut}, nil
	}
	s := &Syncer{Bin: "/opt/homebrew/bin/rsync", run: run}
	info, err := s.CheckBinary(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotBin != "/opt/homebrew/bin/rsync" {
		t.Errorf("bin = %q", gotBin)
	}
	if len(gotArgs) != 1 || gotArgs[0] != "--version" {
		t.Errorf("args = %v", gotArgs)
	}
	if info.Version != "3.2.7" {
		t.Errorf("Version = %q", info.Version)
	}
}

func TestCheckBinaryExecFailure(t *testing.T) {
	run := func(_ context.Context, _ string, _ []string, _ io.Writer) (runResult, error) {
		return runResult{}, errors.New("exec: not found")
	}
	s := &Syncer{run: run}
	_, err := s.CheckBinary(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not found or not runnable") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), `"rsync"`) {
		t.Errorf("error should name the default bin: %v", err)
	}
}

func TestCheckBinaryOpenRsync(t *testing.T) {
	run := func(_ context.Context, _ string, _ []string, _ io.Writer) (runResult, error) {
		return runResult{stdout: openRsyncVersionOut}, nil
	}
	s := &Syncer{run: run}
	info, err := s.CheckBinary(context.Background())
	if err == nil || !strings.Contains(err.Error(), "openrsync") {
		t.Fatalf("err = %v", err)
	}
	if !info.OpenRsync {
		t.Error("OpenRsync should be true")
	}
}

func TestCheckBinaryCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	run := func(ctx context.Context, _ string, _ []string, _ io.Writer) (runResult, error) {
		return runResult{}, ctx.Err()
	}
	s := &Syncer{run: run}
	_, err := s.CheckBinary(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
