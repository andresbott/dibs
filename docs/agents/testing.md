# Testing — strategy, make targets, CI

## Strategy

- Unit tests inject fakes at the seams: `lifecycle.Runner.NewSyncer`/`NewAccessor`/`Now`,
  and the engine's unexported exec `runner` (canned `--list-only`/itemize output —
  engine unit tests never shell out to rsync).
- The pure classifier (`libs/threewayrsync/classify.go`) is table-tested against the
  full three-way decision table plus the converged rule.
- `zarf/e2e` builds a real binary and drives it as a subprocess through full lifecycle
  scenarios; it asserts documented behavior, so it doubles as an executable spec.
  Every scenario runs on all three remote transports via `forEachRemoteFlavor`: a
  plain path, a loopback rsync daemon, and a loopback sshd (the ssh flavor skips
  when `sshd`/`ssh-keygen` are unavailable). See
  [`../../zarf/README.md`](../../zarf/README.md).

## Make targets

| Target | What it runs |
|---|---|
| `make test` | `go test ./... -cover` — the fast suite |
| `make lint` | `golangci-lint run` |
| `make license-check` | `go-licence-detector` against `allowedLicenses.json` / `overrideLicenses.json` |
| `make benchmark` | `go test -bench=.` across all packages |
| `make coverage` | per-package coverage gate, threshold **70%** (`COVERAGE_THRESHOLD`) |
| `make e2e` | `go test -tags e2e ./zarf/e2e/...` — needs `rsync` on PATH, builds a temp binary |
| `make verify` | all of the above, in that order — the full gate before calling work done |

- Coverage is gated on `./app/...`, `./internal/...`, and `./libs/...`;
  `app/metainfo` is excluded (linker-stamped vars only). If a package falls below
  threshold, write the missing tests — never lower the threshold.
- The e2e tests are behind the `e2e` build tag, so `make test` stays fast; plain
  `go test ./...` does not run them.

## Linters (`.golangci.yaml`)

Standard set (errcheck, govet, ineffassign, staticcheck, unused) plus `nolintlint`,
`gocyclo` (min-complexity 20), `nestif` (min-complexity 5), `gosec`, `dupl`.

- Test files (`*_test.go`) are excluded from `nestif`, `dupl`, and `gosec` (G304 fires
  on tests reading temp files they just wrote under `t.TempDir()`).
- `nolint` directives must name the specific linter and carry an explanation
  (`nolintlint` enforces both). Fix the code instead of silencing the tool — a valid
  `//nolint` is rare.

## CI (`.github/workflows/`)

- `test.yml` — on push to main and on PRs: `make test`, `make coverage`, `make e2e`.
- `golangci-lint.yml` / `license-check.yml` — lint and license gates.
- `release.yml` — see [releasing.md](releasing.md).
