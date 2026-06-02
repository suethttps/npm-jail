# CLAUDE.md

Guidance for working in this repository.

## What this is

`npm-jail` is a small Go CLI that runs `npm` commands inside an OS sandbox, so
malicious package lifecycle scripts (`preinstall`/`postinstall`) can't read
secrets (`~/.ssh`, `~/.aws`, …) or write outside the project. Linux uses
[bubblewrap](https://github.com/containers/bubblewrap) (`bwrap`); macOS uses
Apple's legacy `/usr/bin/sandbox-exec` interface. See `README.md` for the
user-facing docs and the full security model.

The tool does **not** reimplement npm: it assembles a platform sandbox command
and `exec`s either `bwrap … -- npm <args>` on Linux or
`sandbox-exec -p <profile> -- npm <args>` on macOS.

## Build & run

Go is provided by `mise` in this repo (there is no system Go):

```bash
mise exec -- go build -o npm-jail .     # build
./npm-jail --dry-run install            # inspect the bwrap line without running
./npm-jail --help
```

Unit tests live in `main_test.go`. Verify changes by:
1. `mise exec -- go test ./...` for unit coverage.
2. `--dry-run` to inspect the generated `bwrap` command line.
3. Functional checks in a scratch dir — run a `package.json` script that
   inspects the filesystem from inside the jail, e.g.:
   ```bash
   npm-jail run inspect   # script does fs.readdirSync(os.homedir()), tries ~/.ssh, etc.
   ```
   This exercises the real threat model (a lifecycle script) end to end.

Before committing, always run the test/lint validation set:
`mise exec -- go test ./...`, `mise exec -- go vet ./...`,
`mise exec -- goreleaser check`, and `git diff --check`. If those pass and the
user has asked to commit, create the commit without asking again; keep unrelated
worktree changes out of the commit.

## Releasing / distribution

- Distribution is binary-only via GitHub releases — users never clone. Install is
  `mise use -g github:suethttps/npm-jail` or direct curl/tar from the GitHub
  release assets. Arch Linux users can install `npm-jail-bin` from the AUR.
- `.goreleaser.yaml` defines the build (Linux and macOS amd64/arm64).
  `.github/workflows/release.yml` runs it on every manually pushed `v*` tag;
  `ci.yml` validates build/vet/goreleaser-config on PRs.
- `.github/workflows/auto-release.yml` creates the next patch tag and publishes
  a release after merges to `master`, but intentionally only for binary-affecting
  changes: `**/*.go`, `go.mod`, `go.sum`, and `.goreleaser.yaml`. Documentation,
  CI-only, and repo metadata changes must not create a new app release by
  themselves.
- If the project grows beyond a single `main.go`, keep new Go files covered by
  the existing `**/*.go` auto-release path filter. If future release artifacts
  depend on new non-Go files, add them explicitly to the auto-release `paths`
  list.
- Archive name is `npm-jail_<Linux|Darwin>_<x86_64|aarch64>.tar.gz` — keep this
  template intact, it's what mise's `github`/ubi backend matches against.
- `.github/scripts/publish-aur.sh` updates the `npm-jail-bin` AUR package after
  releases when the `AUR_SSH_PRIVATE_KEY` repository secret is configured. The
  key's public half must be registered in the maintainer's AUR account.
- `var version` in `main.go` is overridden at release build time via
  `-ldflags -X main.version=<tag>` (exposed by `--version`); local builds say `dev`.

## Conventions

- **Stdlib only.** `go.mod` has zero dependencies; keep it that way (the config
  file is JSON via `encoding/json`, not TOML, specifically to avoid a dep).
- Everything lives in a single `main.go`.
- Code comments and user-facing strings (`usage`, errors) are in English.

## Architecture (`main.go`)

Flow: `parseArgs` → `resolveConfig` (merge file + CLI) →
`buildSandboxCommand` → `exec.Command(...)`.

- `buildSandboxCommand` selects the backend by `runtime.GOOS`: Linux keeps
  `buildBwrapArgs`; macOS uses `buildSandboxExecArgs` and a generated SBPL
  profile for `/usr/bin/sandbox-exec`.
- macOS does not have Linux namespace/tmpfs parity. The backend is mainly
  filesystem/network policy: reads are broadly allowed with explicit sensitive
  path denials, writes are limited to project/cache/temp/explicit `rw` paths.

- `cliFlags` uses `*bool` for `noNet`/`allowGlobal`/`shareHome` to distinguish
  "not given" from "given as false". This is what makes the file↔CLI merge
  predictable — e.g. `--net` (sets the pointer to `false`) can override a
  `"no_net": true` coming from the file.
- `resolveConfig`: file (`.npm-jail`, JSON) is the default layer, CLI overrides.
  `rw`/`ro` lists are the **union** of file + CLI; booleans are overridden.
- `.env*` masking is automatic for npm install-style commands (`install`, `i`,
  `ci`, `add`) and opt-in for other commands through `--hide-env`; `--no-hide-env`
  disables the automatic install masking.
- `buildBwrapArgs` is where the sandbox is defined. **Mount order matters** —
  bwrap applies args sequentially:
  - `--tmpfs $HOME` must come **before** binding the Node toolchain and caches,
    because those live under `$HOME` (e.g. mise) and would otherwise be wiped.
  - the project `cwd` bind also comes after the home tmpfs (cwd is often under
    `$HOME`).

## Gotchas / platform details

- **usr-merge** (Arch, Fedora): `/bin`, `/sbin`, `/lib`, `/lib64` are symlinks to
  `/usr/*`. `addRootEntry` detects this via `Lstat` and recreates the symlink
  with `--symlink`; on non-merged distros it `--ro-bind`s the real dir.
- **DNS** (`addResolvConf`): we `--tmpfs /run`, which breaks the usual
  `/etc/resolv.conf -> /run/systemd/resolve/…` symlink. When the network is
  shared (`!noNet`), we bind the real resolv.conf onto the symlink's *target*
  path (inside the writable `/run` tmpfs), not onto `/etc/resolv.conf` (that path
  is in the read-only `/etc` bind and can't be created).
- **Node toolchain discovery** (`resolveNodeToolchain`): resolves `node` on PATH,
  follows symlinks, and binds the parent-of-bin (the version dir). With mise that
  is `…/installs/node/<ver>`, which also contains `npm`/`npx` (symlinks into
  `lib/node_modules/npm`), so one read-only bind covers all three.
- **`--no-net`** truly isolates the network (no loopback) → DNS fails fast with
  `EAI_AGAIN`. Don't use it for `install` or dev servers that fetch from a CDN.
- `$HOME` is an ephemeral tmpfs, so tools writing to `~/.cache` succeed but their
  cache vanishes on exit. To persist, add the path via `rw` in `.npm-jail`.
