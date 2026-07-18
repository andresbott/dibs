# The sync engine (`libs/threewayrsync`)

Read this before touching anything under `libs/threewayrsync/`. The package is
deliberately domain-neutral — it knows endpoints, manifests, and plans, never profiles,
markers, or config (see [architecture.md](architecture.md) for how lifecycle wraps it).
Design spec: [`../superpowers/specs/2026-07-14-threewayrsync-design.md`](../superpowers/specs/2026-07-14-threewayrsync-design.md).

- **rsync is the sole fingerprint and transfer mechanism.** Trees are enumerated with
  `rsync --list-only` on both sides, so a local path, an `ssh://` host, and an
  `rsync://` daemon module all go through one code path. Go computes the three-way
  classification; rsync applies transfers via `--files-from` (NUL-separated, `--from0`).
- **Change fidelity is size + 1-second mtime** — rsync's own quick-check — *not* content
  hashes. What the engine plans and what rsync transfers therefore never disagree.
  Accepted limitation: files differing in content but colliding on size+mtime read as
  in-sync. (An earlier design used a hybrid size+mtime+SHA-256 scheme in
  `internal/baseline`; it was dropped when the engine moved to rsync listings —
  old state files' hashes are ignored on load and their nanosecond mtimes truncated.)
- **Cancel/resume is stateless.** The base manifest is committed once, atomically, only
  after a fully clean apply; `--partial`/`--times` preserve progress; and a "converged"
  rule (both sides equal → Noop even if both differ from base) makes a re-run after
  cancellation re-derive exactly the remaining work. There are no checkpoint files.
- **The only exported pluggability is `Store`** (load/commit the base manifest) plus its
  optional `Locker` (flock; prevents two concurrent syncs of one profile from
  interleaving). `internal/baseline.ProfileStore` is the dibs implementation.
- **Remote deletes use `rsync --delete-missing-args` against an empty source dir** —
  no remote shell (a daemon has none), no quoting, no ARG_MAX. Requires GNU rsync ≥ 3.1
  on both ends, hence the up-front `CheckBinary` (macOS openrsync fails fast with an
  actionable message). Local deletes are `os.Remove`, re-stat-guarded so a file edited
  between planning and apply is skipped, not lost.
- **Safety valves in the engine:** strict list parsing (a malformed line is an error,
  not a skip — silent skips once turned format drift into phantom deletions); listing
  paths that are absolute or contain `..` are rejected; ssh/daemon URL components are
  validated against option injection (leading `-`, whitespace); the base file is
  fsync'd and a corrupt one surfaces as `ErrCorruptState`.

Whether planned deletes are *applied* is the caller's policy, not the engine's — see
"Delete semantics" in [architecture.md](architecture.md) for the `--allow-deletes` /
wipe-valve rules layered on top by `internal/lifecycle`.
