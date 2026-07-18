# internal/

Domain logic for netcheckout, not importable by other modules.

Planned packages (see [`../GOALS.md`](../GOALS.md)):

- **config** — config schema + YAML loader/saver, path resolution, validation. (implemented)
- **marker** — read/write the remote `.netcheckout.json` marker (the cooperative lock).
- **rsync** — wrapper around the `rsync` binary for copy/sync operations.
- **checkout** — checkout / check-in orchestration and conflict rules.
- **state** — local state cache of active checkouts.

`config` is implemented; the rest are still planned.
