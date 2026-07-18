package threewayrsync

import (
	"testing"
	"time"
)

func TestClassifyPathTable(t *testing.T) {
	type side struct{ present, changed bool }
	cases := []struct {
		name      string
		inBase    bool
		local     side
		remote    side
		converged bool
		want      action
	}{
		{"local edit, remote unchanged", true, side{true, true}, side{true, false}, false, actPush},
		{"remote edit, local unchanged", true, side{true, false}, side{true, true}, false, actPull},
		{"both edited (diverged)", true, side{true, true}, side{true, true}, false, actConflict},
		{"both edited identically (converged)", true, side{true, true}, side{true, true}, true, actNoop},
		{"remote addition", false, side{false, false}, side{true, false}, false, actPull},
		{"local addition", false, side{true, false}, side{false, false}, false, actPush},
		{"both added same path (diverged)", false, side{true, false}, side{true, false}, false, actConflict},
		{"both added identical (converged)", false, side{true, false}, side{true, false}, true, actNoop},
		{"local delete (remote unchanged)", true, side{false, false}, side{true, false}, false, actRemoteDelete},
		{"remote delete (local unchanged)", true, side{true, false}, side{false, false}, false, actLocalDelete},
		{"both unchanged", true, side{true, false}, side{true, false}, false, actNoop},
		{"both deleted", true, side{false, false}, side{false, false}, false, actNoop},
		{"local delete vs remote edit", true, side{false, false}, side{true, true}, false, actConflict},
		{"remote delete vs local edit", true, side{true, true}, side{false, false}, false, actConflict},
		{"not in base, neither present", false, side{false, false}, side{false, false}, false, actNoop},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := classifyPath(c.inBase, c.local.present, c.local.changed, c.remote.present, c.remote.changed, c.converged)
			if got != c.want {
				t.Errorf("classifyPath = %v, want %v", got, c.want)
			}
		})
	}
}

func TestClassifyConvergedBothSidesEqualIsNoop(t *testing.T) {
	base := Manifest{"x": {Size: 1, ModTime: time.Unix(100, 0)}}
	// Both sides changed to the SAME new state => converged, not a conflict.
	local := Manifest{"x": {Size: 2, ModTime: time.Unix(200, 0)}}
	remote := Manifest{"x": {Size: 2, ModTime: time.Unix(200, 0)}}
	plan := Classify(base, local, remote)
	if len(plan.Conflicts) != 0 {
		t.Errorf("converged change must not conflict: %+v", plan)
	}
	if !plan.InSync {
		t.Errorf("converged change must be in sync: %+v", plan)
	}
}

// Directory entries flow through the same decision table with presence-only semantics:
// Equal is unconditionally true for dir/dir, so dirs never register as "changed" and
// mtime drift can neither edit nor conflict.
func TestClassifyDirectories(t *testing.T) {
	t0, t1 := time.Unix(100, 0), time.Unix(999, 0)
	dir0, dir1 := FileState{IsDir: true, ModTime: t0}, FileState{IsDir: true, ModTime: t1}
	base := Manifest{
		"stay":   dir0,
		"rmdirR": dir0, // deleted locally => RemoteDelete
		"rmdirL": dir0, // deleted remotely => LocalDelete
	}
	local := Manifest{
		"stay":   dir1, // mtime drifted: still a noop
		"rmdirL": dir0,
		"newL":   dir0, // created locally => Push
	}
	remote := Manifest{
		"stay":   dir0,
		"rmdirR": dir1, // mtime drifted: still a plain delete, not delete-vs-edit
		"newR":   dir0, // created remotely => Pull
	}
	plan := Classify(base, local, remote)
	if len(plan.Push) != 1 || plan.Push[0] != "newL" {
		t.Errorf("Push = %v, want [newL]", plan.Push)
	}
	if len(plan.Pull) != 1 || plan.Pull[0] != "newR" {
		t.Errorf("Pull = %v, want [newR]", plan.Pull)
	}
	if len(plan.RemoteDeletes) != 1 || plan.RemoteDeletes[0] != "rmdirR" {
		t.Errorf("RemoteDeletes = %v, want [rmdirR]", plan.RemoteDeletes)
	}
	if len(plan.LocalDeletes) != 1 || plan.LocalDeletes[0] != "rmdirL" {
		t.Errorf("LocalDeletes = %v, want [rmdirL]", plan.LocalDeletes)
	}
	if len(plan.Conflicts) != 0 {
		t.Errorf("dirs must never conflict among themselves: %v", plan.Conflicts)
	}
}

// A path that is a file on one side and a directory on the other is a type change:
// Equal is false, so the table lands on conflict (or push/pull when only one side
// diverged from base) — never a silent transfer over the mismatched type.
func TestClassifyFileDirTypeMismatch(t *testing.T) {
	t0 := time.Unix(100, 0)
	file := FileState{Size: 1, ModTime: t0}
	dir := FileState{IsDir: true, ModTime: t0}

	// Both added the same path with different types => conflict.
	plan := Classify(Manifest{}, Manifest{"x": file}, Manifest{"x": dir})
	if len(plan.Conflicts) != 1 || plan.Conflicts[0] != "x" {
		t.Errorf("both-added type mismatch must conflict: %+v", plan)
	}

	// Base file replaced by a dir locally, remote unchanged => push (local edit).
	plan = Classify(Manifest{"x": file}, Manifest{"x": dir}, Manifest{"x": file})
	if len(plan.Push) != 1 || plan.Push[0] != "x" {
		t.Errorf("local type change with unchanged remote must push: %+v", plan)
	}

	// Base file: deleted locally, replaced by a dir remotely => delete-vs-edit conflict.
	plan = Classify(Manifest{"x": file}, Manifest{}, Manifest{"x": dir})
	if len(plan.Conflicts) != 1 || plan.Conflicts[0] != "x" {
		t.Errorf("local delete vs remote type change must conflict: %+v", plan)
	}
}

func TestClassifyBucketsPushPullDelete(t *testing.T) {
	t0 := time.Unix(100, 0)
	t1 := time.Unix(200, 0)
	base := Manifest{
		"push.txt": {Size: 1, ModTime: t0},
		"pull.txt": {Size: 1, ModTime: t0},
		"del.txt":  {Size: 1, ModTime: t0},
	}
	local := Manifest{
		"push.txt": {Size: 2, ModTime: t1}, // locally edited
		"pull.txt": {Size: 1, ModTime: t0}, // unchanged
		// del.txt removed locally; remote unchanged => RemoteDelete
	}
	remote := Manifest{
		"push.txt": {Size: 1, ModTime: t0}, // unchanged
		"pull.txt": {Size: 2, ModTime: t1}, // remotely edited
		"del.txt":  {Size: 1, ModTime: t0}, // unchanged
	}
	plan := Classify(base, local, remote)
	if len(plan.Push) != 1 || plan.Push[0] != "push.txt" {
		t.Errorf("Push = %v", plan.Push)
	}
	if len(plan.Pull) != 1 || plan.Pull[0] != "pull.txt" {
		t.Errorf("Pull = %v", plan.Pull)
	}
	if len(plan.RemoteDeletes) != 1 || plan.RemoteDeletes[0] != "del.txt" {
		t.Errorf("RemoteDeletes = %v", plan.RemoteDeletes)
	}
	if plan.InSync {
		t.Errorf("plan should not be in sync: %+v", plan)
	}
}
