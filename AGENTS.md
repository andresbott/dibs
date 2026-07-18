# netcheckout — What this app is for

This file captures the core user flow the app exists to serve. Every feature should map
back to a step in this flow. The detailed design spec lives in [GOALS.md](GOALS.md).

## The user flow

1. **Check out** a directory on a server (the *remote root*), optionally limiting the
   scope to a subset of its subdirectories. This claims the directory with a marker so
   other machines/users know it's taken.
2. **Sync down**: pull all in-scope files from the remote to the local file system
   (fast local disk).
3. **Work locally**: edit, add, move, rename, and delete files in the local working
   copy — at local-disk speed, without touching the remote.
4. **Sync back** from time to time: push local changes to the server. The remote may
   occasionally have changed too (uncommon, but it happens), so a sync must also pull
   remote-side changes and stop on genuine same-file conflicts rather than clobbering
   either side.
5. **Check in** when done: verify local and remote are fully in sync, then release the
   checkout (remove the marker). The remote is the canonical copy again.

## The remote can be

- an **SMB folder mounted locally** (macOS or Linux) — a plain filesystem path,
- an **rsync daemon** (`rsync://host/module/path`),
- an **rsync-over-ssh** location (`ssh://user@host/path`).

The sync engine must work identically across all three; `rsync` does the actual data
movement everywhere.

## Ground rules that follow from the flow

- Only `sync` moves file data. `checkout` writes the marker and an empty baseline;
  `checkin` verifies clean and removes the marker. Neither copies files.
- The remote keeps its files throughout — there is never a window where the only copy
  of the data is local.
- Server-side changes during a checkout are expected to be rare but must be handled
  safely: three-way merge against the checkout baseline, conflicts stop the sync.
- The lock is cooperative and per-profile: one marker at the remote root, owned by one
  machine, covering the whole checkout regardless of subdirectory scope.
