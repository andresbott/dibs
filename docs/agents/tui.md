# TUI conventions (`app/tui`)

The TUI is a thin bubbletea frontend over `internal/lifecycle` — it never duplicates
orchestration (see [architecture.md](architecture.md)).

- **One keybinding convention app-wide**
  ([`../superpowers/specs/2026-07-04-tui-keybinding-convention-design.md`](../superpowers/specs/2026-07-04-tui-keybinding-convention-design.md)):
  `esc` = back/cancel one level everywhere; `q` quits only from the main list; `w`/`s`
  (+ arrows) move; `enter`/`space` activate. Destructive dialogs focus **Cancel** by
  default; `y` confirms directly, `n`/`esc` cancels.
- **Action options are checkboxes in each action's confirm dialog** (steal lock, allow
  deletes, local wins, clean, abandon) — not footer toggles (the 2026-07-11 design's
  footer-toggle idea was superseded). Failures render in the Activity box; there is no
  inline "force?" prompt.
- **All I/O runs off the UI thread** as bubbletea commands; results carry the profile
  name and a sequence stamp so stale/raced results are recognized and dropped. Streaming
  actions (checkout/sync/checkin) feed the Activity box live through the same
  `OnApply` events the CLI prints.
- The two-tier status display: sanity marks (`✓`/`✗`/`…`/`?`) appear in Details
  automatically; the heavy rsync Status runs only on explicit action.
- **The profile form's remote location is a type selector** (Local / rsync / ssh) over
  one stable set of inputs (`form.go` `idx*` constants) — all inputs always exist so
  switching type never loses typed values; `values()` reads only the selected kind's
  fields and composes/decomposes `RemoteRoot` via `config.SplitRemoteRoot` /
  `config.BuildRsyncRemoteRoot`. The rsync kind's Browse opens the async daemon
  browser (`remotepicker.go`: modules → directories, seq-stamped results, injectable
  `moduleLister`/`dirLister` seams); the local `dirPicker` stays synchronous
  (`os.ReadDir` needs no command).
