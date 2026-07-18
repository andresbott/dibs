# zarf/

Supporting files that are not application code.

- **`e2e/`** — end-to-end lifecycle tests: each test builds a temporary `dibs`
  binary and drives it as a subprocess (checkout → sync → checkin scenarios,
  conflict handling), on three remote transports — a plain path, a loopback
  rsync daemon, and a loopback sshd. Requires `rsync`; the ssh flavor also
  needs `sshd` and `ssh-keygen` (skips otherwise). Run with `make e2e` (also
  part of `make verify`).
- **`sample/`** — a ready-to-read sample config plus matching local/remote root
  folders for three profiles, showing the config format and the `subpaths`
  option. See [`sample/README.md`](sample/README.md).
