English | [简体中文](system-update.zh-CN.md)

# Application Upgrade & Artifact Library

SublinkPro ships with a self-upgrade system backed by a local build-artifact library. It downloads release binaries for the exact running platform, verifies them with a test-mode trial run, swaps the binary atomically, and restarts the service — with one-click rollback to any previously stored version.

## Overview

- **Artifact library**: downloaded and replaced-out binaries are kept under `<db_path>/updater/artifacts/`, indexed by `versions.json` (a JSON ledger recording version, platform, size, sha256, status, and timestamps).
- **Upgrade sources** (configurable in the web UI):
  - **JSON manifest** (recommended): one URL describing multiple versions, each with per-platform files (`os`/`arch`/`url`/optional `sha256`).
  - **GitHub Releases**: enumerate all historical releases and assets of a repository directly, platform-matched by filename.
  - **Single-file template URL**: locate a file via `{os}`, `{arch}`, `{ext}`, `{version}` placeholders.
- **Manual upload upgrade**: upload an artifact from the web UI and it runs the exact same pipeline as remote upgrades — test → swap → restart.
- **Platform detection**: the app matches its own `GOOS/GOARCH` (windows/linux/darwin × amd64/arm64, etc.) against the source and only offers installable files.
- **Test mode**: before anything is installed, the candidate binary is executed with `--version`. It must exit 0 within 20s with non-empty output; otherwise the artifact is discarded and nothing changes.
- **Safe swap**: the current binary is renamed to `.old`, the new file is copied into place, and the process restarts (Unix: in-place `exec`; Windows: detached delayed starter + graceful shutdown). A failed swap rolls back automatically.

## Manifest format

```json
{
  "latest": "v1.6.0",
  "versions": [
    {
      "version": "v1.6.0",
      "notes": "Release notes (optional)",
      "files": [
        { "os": "windows", "arch": "amd64", "url": "https://example.com/sublinkpro-v1.6.0-windows-amd64.exe", "sha256": "<hex, optional>" },
        { "os": "linux",   "arch": "amd64", "url": "https://example.com/sublinkpro-v1.6.0-linux-amd64.tar.gz" },
        { "os": "linux",   "arch": "arm64", "url": "https://example.com/sublinkpro-v1.6.0-linux-arm64.zip" }
      ]
    }
  ]
}
```

Files may be raw binaries or `.zip` / `.tar.gz` archives containing the binary. When `sha256` is provided it is verified after download.

> [!TIP]
> The release workflow (`.github/workflows/build-release.yml`) generates this manifest automatically for every tagged release — including per-file sha256 — and attaches it as a `versions.json` release asset. The stable URL to configure in the UI is:
> `https://github.com/ZeroDeng01/sublinkPro/releases/latest/download/versions.json`
>
> The manifest lists the newest release; older versions remain rollback-able through the local artifact library, which snapshots the running binary on every upgrade.

## Usage

Open **System → Changelog (/system/updates)**:

1. **Upgrade Source**: pick the source type, fill in the URL, optionally enable mihomo-proxy downloads, choose how many artifacts to keep, and save.
   - **JSON manifest** — a `versions.json` URL describing versions and per-platform files (recommended).
   - **GitHub Releases** — set `owner/repo` (plus an optional token for private repos / rate limits). The app enumerates ALL historical releases via the GitHub API and matches release assets to the running platform by filename (`-linux-amd64`, `-windows-x86.exe`, `darwin-arm64`, `linux_x86_64`, `win64` …), so no `versions.json` is required.
   - **Template URL** — placeholders `{os} {arch} {ext} {version}`; cannot list history.
2. **Remote Version Library**: click *Check versions* to list everything published; versions without a file for the current platform are marked not installable. Pick any version to upgrade to (or *Upgrade to latest*).
3. **Local Artifact Library**: shows every stored artifact with status (active / backup / archived). Use ↩ to roll back or 🗑 to delete old ones (the active version cannot be deleted).
4. **Upload & upgrade**: click *Upload & upgrade* to send a local binary or zip/tar.gz archive (≤512MB) directly. After upload the full pipeline runs automatically — test-mode trial run → snapshot current version → atomic swap → restart — with zero manual steps; a failed upload never touches the running binary. An optional version label overrides the one reported by the trial run.
5. Progress is reported to the **Task Center** (`system_update` tasks) over SSE; the page polls upgrade state every 3 seconds.

The first successful upgrade snapshots the currently running binary into the library as a backup, so a rollback point always exists.

## Rollback

Clicking rollback on any non-active artifact runs the same pipeline as an upgrade — integrity check, test-mode verification, atomic swap, restart — but restores the selected historical version. The displaced active version returns to `backup` status.

## Files on disk

```
<db_path>/updater/
├── config.json    # upgrade source settings
├── versions.json  # artifact ledger (schemaVersion/current/artifacts[])
├── state.json     # last operation result (survives restart)
├── artifacts/     # stored binaries: {version}-{os}-{arch}{ext}
└── staging/       # temporary download/extraction area
```

## Notes & limitations

- Demo mode blocks all write operations (save config, upgrade, rollback, delete).
- On Unix the restart uses `syscall.Exec`; open sockets close automatically and the new binary binds the same port. In Docker (binary as PID 1) this works without restarting the container.
- On Windows the service spawns a detached helper that starts the new binary ~2s later, after the old process has released the port via graceful shutdown. If the process runs under a service manager that does not respawn children, keep using the built-in path.
- Version comparison follows semver-lite rules: `v` prefix-insensitive, numeric segment ordering, pre-release (`-rc.1`) sorts below the final release.
- The API surface is documented in [skill-sublinkpro/reference/api.md](../../skill-sublinkpro/reference/api.md) under `/api/v1/updater/*`.
