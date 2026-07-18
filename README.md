# netcheckout

A CLI utility to **check out** and **check in** work directories over network drives
(e.g. a locally-mounted Samba/NAS share), using `rsync` to copy files between a remote
root and a local working copy, and leaving a marker behind so others can see a folder is
checked out and by whom.

See [`GOALS.md`](./GOALS.md) for the full design.

> Status: `version`, `list`, and the profile-management TUI are implemented;
> checkout/check-in/status are not yet — see [`GOALS.md`](./GOALS.md).

## Install

### macOS (Homebrew)

A macOS cask is published into this repository on every tagged release. Because the repo
isn't named `homebrew-*`, tap it with an explicit URL, then install:

```bash
brew tap andresbott/tap https://github.com/andresbott/netcheckout
brew install --cask andresbott/tap/netcheckout
```

`rsync` is pulled in as a dependency, and `brew upgrade` will track future releases.

### Debian / Ubuntu

Download the `.deb` for your architecture from the
[releases page](https://github.com/andresbott/netcheckout/releases) and install it
(this also pulls in `rsync`):

```bash
sudo apt install ./netcheckout_*_amd64.deb
```

### Other

Grab a prebuilt `tar.gz` archive from the
[releases page](https://github.com/andresbott/netcheckout/releases).

## Usage

Running `netcheckout` with no arguments opens an interactive TUI for managing profiles
(a profile is a named `local_root` / `remote_root` pair):

- `a` — add a profile
- `e` — edit the selected profile
- `d` — delete the selected profile (with confirmation)
- `enter` — reveal actions for the selected profile (checkout/check-in/status/sync coming soon)
- with actions showing: `↑`/`↓`/`w`/`s` select an action, `enter` runs it (coming soon), `esc` returns to the list
- in the add/edit dialog: `tab`/`↑`/`↓`/`←`/`→` move between fields, `enter`/`space` activates, `esc` cancels
- in the delete-confirmation dialog: `tab`/`←`/`→` move between Delete/Cancel, `enter`/`space` activates, `y` deletes directly, `n`/`esc` cancels
- `esc`/`q` — quit from the list

When stdout isn't a terminal (e.g. piped or redirected), `netcheckout` prints the
profile list as plain text instead of opening the TUI. `netcheckout list` always prints
that plain-text list, TUI or not:

```bash
netcheckout          # interactive TUI (plain-text list when not a terminal)
netcheckout list     # always prints the profile list as plain text
```

### Configuration file

Profiles are stored in a YAML file at `os.UserConfigDir()/netcheckout/config.yaml`:

| OS | Default path |
|---|---|
| Linux | `~/.config/netcheckout/config.yaml` |
| macOS | `~/Library/Application Support/netcheckout/config.yaml` |
| Windows | `%AppData%\netcheckout\config.yaml` |

Override the location with `--config <path>` or the `$NETCHECKOUT_CONFIG` environment
variable.

A profile may optionally scope itself to a few sub-folders with a `subpaths` list —
relative paths under *both* roots (nested allowed; omit for the whole root). See
[`GOALS.md`](./GOALS.md) and `zarf/sample/config.yaml` for an example.

The list is a hard scope by design: folders that exist (or later appear) on the remote
but are not listed are never pulled in — add them to `subpaths` when you want them.
Local content outside the scope is different: it would be stranded by a scoped sync,
so `status` and the TUI warn about it and `sync`/`checkin` refuse to run until it is
listed, moved, or removed.

## Develop

Requires Go 1.26+. The full toolchain also needs `golangci-lint`, `goreleaser`, and
`go-licence-detector`; at runtime `rsync` must be on `PATH`.

```bash
make verify       # test → license-check → lint → benchmark → coverage
make help         # list all targets
```

### Run

```bash
make run          # or: go run main.go version
```

### Build

```bash
make build        # goreleaser snapshot build for the current OS/arch → ./dist
# or plain go:
go build ./...
```

### Release

Releases are published by pushing a semver tag from a clean `main` branch. The tag
triggers the [Release workflow](./.github/workflows/release.yml), which runs GoReleaser
to build and publish the archives, `.deb`, and Homebrew cask.

```bash
make tag version="v1.2.3"
```

`make tag` refuses to run unless you're on `main` with a clean working tree, then creates
and pushes the `vX.Y.Z` tag.
