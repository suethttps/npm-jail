# CLAUDE.md

Guidance for working in this repository.

## What this is

`npm-jail` is a small Go CLI that runs `npm` commands inside a
[bubblewrap](https://github.com/containers/bubblewrap) (`bwrap`) sandbox, so
malicious package lifecycle scripts (`preinstall`/`postinstall`) can't read
secrets (`~/.ssh`, `~/.aws`, …) or write outside the project. See `README.md`
for the user-facing docs and the full security model.

The tool does **not** reimplement npm: it assembles a `bwrap` argument list and
`exec`s `bwrap … -- npm <args>`.

## Build & run

Go is provided by `mise` in this repo (there is no system Go):

```bash
mise exec -- go build -o npm-jail .     # build
./npm-jail --dry-run install            # inspect the bwrap line without running
./npm-jail --help
```

There is no test suite. Verify changes by:
1. `--dry-run` to inspect the generated `bwrap` command line.
2. Functional checks in a scratch dir — run a `package.json` script that
   inspects the filesystem from inside the jail, e.g.:
   ```bash
   npm-jail run inspect   # script does fs.readdirSync(os.homedir()), tries ~/.ssh, etc.
   ```
   This exercises the real threat model (a lifecycle script) end to end.

## Conventions

- **Stdlib only.** `go.mod` has zero dependencies; keep it that way (the config
  file is JSON via `encoding/json`, not TOML, specifically to avoid a dep).
- Everything lives in a single `main.go`.
- Code comments are in Portuguese; user-facing strings (`usage`, errors) are in
  Portuguese too. README and this file are in English.

## Architecture (`main.go`)

Flow: `parseArgs` → `resolveConfig` (merge file + CLI) → `buildBwrapArgs` →
`exec.Command("bwrap", …)`.

- `cliFlags` uses `*bool` for `noNet`/`allowGlobal`/`shareHome` to distinguish
  "not given" from "given as false". This is what makes the file↔CLI merge
  predictable — e.g. `--net` (sets the pointer to `false`) can override a
  `"no_net": true` coming from the file.
- `resolveConfig`: file (`.npm-jail`, JSON) is the default layer, CLI overrides.
  `rw`/`ro` lists are the **union** of file + CLI; booleans are overridden.
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
