# 1.0 release readiness

## Docs (broken / stale)

- [ ] GOALS.md was deleted in f6a4370 but is still referenced: README (3x), AGENTS.md,
      internal/README.md, and ~15 code comments citing sections (marker.go, ident.go,
      baseline.go, harness_test.go, ...). Restore it, move to docs/, or rewrite references.
- [ ] README status line is wrong: says checkout/check-in/status "not yet implemented"
      and TUI actions "coming soon" — all exist now.
- [ ] README documents no CLI commands beyond `list`. Document checkout, sync
      (--allow-deletes semantics, pending deletes, wipe valve), checkin, status.
- [ ] internal/README.md lists "planned packages" with names that no longer match
      reality; rewrite for the actual packages.
- [ ] zarf/README.md says "Empty for now" but zarf holds sample data + the e2e suite.
- [ ] README `make verify` description omits e2e (now part of verify).

## Unfinished work / known gaps

- [ ] Deferred test items: loopback-ssh e2e tests; cancel/resume + OnEvent e2e
      coverage (daemon_test.go covers the rsync-daemon leg only).




## Name 
- [ ] rename repo and files to dibs
