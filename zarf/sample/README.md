# Sample netcheckout config

A ready-to-read example showing the config format and the local/remote-root
layout for three profiles (`photos`, `work`, and `music`).

- `config.yaml` — the sample configuration (an `identity` plus three profiles).
- `photos/`, `work/`, `music/` — each profile's `local/` (fast working copy) and
  `remote/` (canonical copy on the network share) roots.
- `photos` acts on its whole root; `work` and `music` set `subpaths`, so only the
  listed folders under each root are in scope (see the matching folders under
  each `remote/`).

Roots in `config.yaml` must be **absolute** paths. The example uses a
`/path/to/netcheckout/...` placeholder — replace it with the absolute path to
your checkout to try it against these folders:

```bash
netcheckout --config zarf/sample/config.yaml list
```

Run without `--config` and `netcheckout` reads `$NETCHECKOUT_CONFIG`, or the OS
config dir (Linux `~/.config/netcheckout/config.yaml`, macOS
`~/Library/Application Support/netcheckout/config.yaml`, Windows
`%AppData%\netcheckout\config.yaml`).
