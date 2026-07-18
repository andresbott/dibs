# Releasing — tags, GoReleaser, packaging

## How a release happens

Push a semver tag from a clean `main` branch:

```bash
make tag version="v1.2.3"
```

`make tag` refuses unless you're on `main` with a clean working tree, then creates and
pushes the annotated `vX.Y.Z` tag (deleting any stale local/remote tag of the same name
first). The tag triggers `.github/workflows/release.yml`, which checks out with full
history (the changelog needs it) and runs `goreleaser release --clean` with the default
`GITHUB_TOKEN` (repo `contents: write` is enough — the cask is published into this same
repo).

## What GoReleaser produces (`.goreleaser.yaml`)

- **Binaries:** linux + darwin, amd64 + arm64, `CGO_ENABLED=0` (rsync is shelled out,
  so the binary is pure Go and statically linked), `-trimpath`, stripped.
- **Version stamping:** `-X` ldflags set `Version`, `BuildTime`, and `ShaVer` in
  `app/metainfo` — that package holds linker-stamped vars only and is excluded from
  coverage.
- **Archives:** `tar.gz` per OS/arch with `uname`-compatible names
  (`dibs_Linux_x86_64.tar.gz`, …), bundling LICENSE and README.
- **`.deb`** (linux only) with `rsync` as a runtime dependency and a lintian override
  for the statically-linked binary.
- **Homebrew cask** published into this repo at `Casks/dibs.rb` (the repo isn't named
  `homebrew-*`, so users tap it by explicit URL — see README). macOS-only; declares the
  `rsync` formula as a dependency. The darwin binary isn't code-signed/notarized, so a
  post-install hook strips the `com.apple.quarantine` attribute to avoid a
  "dibs is damaged" Gatekeeper error.
- **Changelog** excludes `docs:` and `test:` commits.

## Local builds

```bash
make build    # goreleaser snapshot build for the current OS/arch → ./dist
make clean    # remove ./dist
```

Snapshot versions are `{next-patch}-snapshot`; no tag or publish involved.
`go build ./...` works too, but produces an unstamped binary (`dibs version` shows
zero values).
