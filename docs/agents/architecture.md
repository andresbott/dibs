# Architecture — layering, lifecycle semantics, guards

The core user flow lives in [`../../AGENTS.md`](../../AGENTS.md). This file distills the
design and implementation decisions made across the design/plan documents in
[`../superpowers/`](../superpowers/) so they can be consulted without re-reading them.
(The historical design spec, GOALS.md, has been removed; those documents' references
to its numbered sections point at a file that no longer exists.)
The superpowers docs are historical records — where they contradict this file or the
code, the code wins (several were superseded mid-project; the ones that matter are
noted below).

Related agent docs: [sync-engine.md](sync-engine.md) (the `libs/threewayrsync`
internals), [testing.md](testing.md), [releasing.md](releasing.md), [tui.md](tui.md).

## Layering

```
app/cmd (cobra CLI)   app/tui (bubbletea)
        \                 /
       internal/lifecycle          ← the single orchestration seam
       /       |        \
 internal/marker  internal/baseline  internal/{status,sanity,localstat,ident,config}
        \        |       /
        libs/threewayrsync          ← generic three-way rsync engine, no dibs types
```

- **`internal/lifecycle` is the only place checkout/sync/checkin logic lives.** The CLI
  and the TUI are both thin frontends over it; neither duplicates orchestration. It
  returns a `Report`, streams per-file progress through `Options.OnApply`, and takes its
  dependencies (`NewSyncer`, `NewAccessor`, `Now`) as injectable fields on `Runner` so
  tests never shell out to rsync.
- **`libs/threewayrsync` is deliberately domain-neutral.** It knows endpoints, manifests,
  and plans — never profiles, markers, or config. It was designed standalone-first
  (spec: `2026-07-14-threewayrsync-design.md`) and then adopted by lifecycle; keeping it
  free of dibs types preserves the option of lifting it into its own module.
  See [sync-engine.md](sync-engine.md) for its internals.
- **`internal/status` runs the same engine as sync** (a read-only `Diff`), so the preview
  and the action can never diverge. `internal/sanity` is the cheap stat-only tier
  (roots exist / marker present / subpaths exist) that runs in the TUI background;
  `status` is the heavy rsync tier that runs on demand.

## Delete semantics (redesigned 2026-07-17 — supersedes the fraction valve)

The original guard was a *fraction* valve (block >50% deletions, waivable). That shape
was wrong — below the threshold deletes propagated silently, above it the waiver pushed
through even a 100% wipe. Current semantics
(`../superpowers/specs/2026-07-17-allow-deletes-redesign-design.md`):

- **`--allow-deletes` off (default): a sync never deletes a file, period.** Planned
  deletes are skipped at apply time and reported as *pending*; transfers still apply.
  Pending deletes are not resurrected (the base keeps the old entry) — every run
  re-plans them until a run with the flag on carries them out.
- **Flag on: deletes apply — except a full wipe, which nothing waives.** A plan that
  would delete 100% of the previously synced in-scope files on one side is refused
  unconditionally (`WouldWipeError`). The sanctioned exit from a deliberately emptied
  local copy is `checkin --abandon` + re-checkout.
- **Checkin blocks on pending deletes** for free: its read-only `Diff` still plans them,
  so `InSync` stays false.

## Checkout is marker-only (supersedes the earlier pull-on-checkout design)

The 2026-07-11 design had `checkout` pull files. It landed, then was reshaped: checkout
now writes the marker and an **empty baseline** and copies nothing — `sync` is the single
data-moving command. The empty baseline makes the first sync classify every remote file
as a fresh pull, never as a delete against a phantom snapshot. Consequences worth
remembering:

- The "local target must be vacant" guard is absolute (`--force` does not bypass it).
- A checkout on a profile this machine already holds *widens* the relpath envelope under
  the same lock; an already-covered relpath is refused ("use sync").
- Claim verification: after writing the marker, checkout re-reads it — two machines
  racing past the "no marker" check is detectable (last write wins; the loser rolls its
  baseline back). The window can be shrunk, not closed, over SMB/rsync.
- **Checkin without `--clean` keeps the baseline as a *released* token**
  (`State.ReleasedAt` set). A released state is treated as absent by sync/checkin
  preflight, checkout widening, and status — only `checkout --resume` consumes it.
  `--clean` (and `--abandon --clean`) removes the state along with the copy.
- **`checkout --resume` adopts a kept local copy without re-downloading:** it
  lists the local tree through the engine (same scope/exclude/ignore as sync) and
  requires every entry to match the released base exactly. Matching entries become
  the new baseline; base entries missing locally are pruned (first sync re-pulls);
  any local-only or differing path refuses with the paths listed — the base is the
  arbiter of *which side moved*, so remote drift never blocks a resume (it pulls or
  mirrors on the first sync) while local drift always does (only the user can
  arbitrate it). The validation is purely local, so all transports behave
  identically. Lock semantics are untouched: a foreign marker still refuses
  (`--force` to steal), a self-held marker still widens. The vacancy guard's
  refusal hints at `--resume` when a matching released token exists. A crash window
  exists: if the process dies after reactivating the baseline but before writing the
  marker, the state is active with no marker and a non-empty copy — recovery is to
  remove the local state or copy, then plain checkout. A downgrade hazard also
  exists: an older dibs binary reading a state file carrying `released_at` ignores
  the unknown field and treats the state as active — a binary downgrade while a
  released token exists risks merging against the stale base.
- Known blind spot (inherited, not new): the fingerprint is size+mtime at 1-second
  resolution, so an edit preserving both passes validation — the same trust every
  sync already places in rsync's quick-check.

## Guards accumulated in lifecycle preflight

Each was added after a concrete failure mode was identified; do not remove casually:

- **Unlisted-local guard** (`sanity.UnlistedLocal`): local content outside the declared
  `subpaths` would be silently stranded by scoped syncs — warning on `status`, blocking
  on `sync`/`checkin`. Local side only, by design: unlisted *remote* content simply stays
  remote (`subpaths` is a hard scope, not a live view). Entries matching the profile's
  `ignore` patterns (`.DS_Store` etc., see below) are invisible to this guard and to the
  envelope guard — sync never touches them, so they can't strand work.
- **Envelope guard** (`sanity.UnlistedOutside`): the same hazard one level down — content
  inside a declared subpath but outside the *checked-out* relpaths, invisible to
  checkin's in-sync verification (which `--clean` would then delete).
- **Root binding:** the baseline records the roots it was taken from; a profile whose
  roots were edited since checkout is refused rather than merged against the wrong trees.
- **Relpath containment:** an explicit sync relpath must lie inside the checkout
  envelope; an explicit checkout relpath must lie inside the declared subpaths.
- **`--clean` target validation:** refuses `/`, depth ≤ 2 paths, and the home directory —
  a config typo must not aim `os.RemoveAll` at a catastrophic target.
- `checkin --abandon` deliberately skips the unlisted/baseline/root-binding guards
  (a wedged guard is exactly what one abandons out of) but never the ownership check.

## Ignore patterns (added 2026-07-19)

Per-profile `ignore:` lists slash-free `path.Match` globs (defaults for new TUI-created
profiles: `.DS_Store`, `.directory`) for file-manager metadata droppings. A path is
ignored when any segment matches, so an ignored dir name covers its subtree. The engine
partitions them out Go-side (see [sync-engine.md](sync-engine.md)) and reports them as
their own **ignored** category in status/sync (CLI and TUI); they never transfer, never
delete, never enter the base, and never block checkin. The sanity guards and localstat
skip them too. Checkout's vacant-target guard deliberately still counts them (data
safety kept strict).

## Still deferred

- Loopback-ssh e2e coverage; cancel/resume + OnEvent e2e coverage beyond the
  rsync-daemon leg (see TODO.md).
- Content-hash manifests as an opt-in detection mode (see the engine limitation in
  [sync-engine.md](sync-engine.md)).
- Hoisting the duplicated CLI/TUI diff-mark formatting (`app/cmd/status.go` and
  `app/tui/profile.go` each carry a copy — intentional for now).
