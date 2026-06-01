# npm-jail

Runs `npm` commands inside a [bubblewrap](https://github.com/containers/bubblewrap)
(`bwrap`) sandbox. Inspired by [ai-jail](https://github.com/akitaonrails/ai-jail),
but focused solely on npm.

The target is the most common attack vector in the npm ecosystem: **lifecycle
scripts** (`preinstall`/`postinstall`) of malicious packages that run arbitrary
code on your machine during `npm install`. Inside the jail that code can't see
your `~/.ssh`, `~/.aws`, `~/.gnupg`, shell history, nor write outside the project.

## Security model (default, no flags)

| Resource | Policy |
|---|---|
| `$HOME` | **empty tmpfs** — `.ssh`, `.aws`, `.gnupg`, tokens, history: don't exist inside the jail |
| Project directory (`cwd`) | **read-write** |
| `/usr`, `/etc`, `/opt` | **read-only** |
| Node toolchain (node/npm/npx) | **read-only** (read-write with `--allow-global`) |
| `~/.npm` (cache) | **read-write** (reuses downloads) |
| `~/.npmrc` | **read-only**, mounted only if it exists |
| PID / UTS / IPC / cgroup | isolated namespaces |
| Network | **shared with the host** by default (`npm install` needs it); use `--no-net` to isolate |

> ⚠️ Not a VM. It relies on the correctness of the kernel and of bwrap; it's
> **one layer** of defense-in-depth, not an absolute boundary. Network being open
> by default means a malicious script can still exfiltrate over the network — use
> `--no-net` when you don't need to download anything.

## Requirements

- Linux with `bwrap` (bubblewrap) — `pacman -S bubblewrap` / `apt install bubblewrap`
- `node`/`npm` on `PATH` (tested with Node via [mise](https://mise.jdx.dev/))

## Install

You don't need to clone the repo. Pick one:

### mise (recommended)

```bash
mise use -g github:suethttps/npm-jail
```

`mise` resolves the latest GitHub release, downloads the right binary for your
platform, and puts it on `PATH`. Pin a version with `github:suethttps/npm-jail@v0.1.0`.

### curl

```bash
curl -fsSL https://raw.githubusercontent.com/suethttps/npm-jail/master/install.sh | sh
```

Installs into `~/.local/bin` by default. Override with `NPM_JAIL_BIN_DIR` or pin a
version with `NPM_JAIL_VERSION=v0.1.0`.

Both methods fetch a prebuilt binary from the [GitHub releases](https://github.com/suethttps/npm-jail/releases).

## Build from source (development only)

Go 1.26+. In this repository Go is provided by `mise`:

```bash
mise exec -- go build -o npm-jail .
install -Dm755 npm-jail ~/.local/bin/npm-jail   # optional
```

## Usage

```bash
npm-jail [npm-jail flags] <npm arguments>
```

```bash
npm-jail install express           # install inside the sandbox
npm-jail ci                         # clean install (lockfile)
npm-jail --no-net run build         # offline build, no network at all
npm-jail --rw ./out run package     # additionally allow ./out to be written
npm-jail --dry-run install          # just print the bwrap command line
```

### npm-jail flags

They must come **before** the npm arguments. The first unrecognized token (or a
`--`) ends the jail flags and everything from there on is passed to npm.

| Flag | Effect |
|---|---|
| `--no-net` | Isolates the network (`--unshare-net`). Good for offline builds. |
| `--net` | Forces the network **on** (overrides `no_net` from `.npm-jail`). |
| `--rw PATH` | Mounts an extra `PATH` read-write (repeatable). |
| `--ro PATH` | Mounts an extra `PATH` read-only (repeatable). |
| `--allow-global` | Makes the Node toolchain read-write (allows `npm i -g`). |
| `--share-home` | Does **not** tmpfs `$HOME` (exposes the real home). Insecure; debugging only. |
| `--no-config` | Ignores the project's `.npm-jail` file. |
| `--init` | Creates a sample `.npm-jail` in the current directory and exits. |
| `--verbose`, `-v` | Prints the full `bwrap` command line before running. |
| `--dry-run` | Prints the `bwrap` command line and exits without running. |
| `--help`, `-h` | Help. |

## Per-project config (`.npm-jail`)

A `.npm-jail` file (JSON) in the project directory is read automatically, so you
don't repeat flags every time. Generate a skeleton with `npm-jail --init`:

```json
{
  "no_net": false,
  "allow_global": false,
  "rw": ["./out"],
  "ro": ["~/.config/something"]
}
```

Precedence: **CLI flags win** over the file. The `rw`/`ro` lists are the **union**
of file + CLI; CLI booleans override (`--net` forces the network on even with
`"no_net": true`). Use `--no-config` to ignore the file. Relative paths are
resolved from the project; `~/` expands to `$HOME`.

## Releasing

Releases are fully automated by [GoReleaser](https://goreleaser.com) via GitHub
Actions. Every merge to `master` runs `auto-release`, which tests the project,
creates the next patch tag (`v0.1.0`, then `v0.1.1`, etc.), and publishes the
GitHub release.

Manual releases are still supported by pushing a `v*` tag:

```bash
git tag v0.1.0
git push origin v0.1.0
```

The release workflows build the Linux `amd64`/`arm64` binaries, package them as
`npm-jail_Linux_<arch>.tar.gz` (the naming `mise`/`ubi` auto-detect), generate
checksums and a changelog, and publish the GitHub release, which is what both
install methods above consume. Test the build locally without publishing with
`goreleaser release --snapshot --clean`.

## How it works

`npm-jail` doesn't call npm directly: it assembles the `bwrap` argument list, sets
up the isolated filesystem, and then runs `npm <args>` inside it. See exactly what
gets mounted with `--dry-run`.

Portability detail: on *usr-merge* distros (Arch, Fedora…), `/bin`, `/sbin`,
`/lib`, `/lib64` are symlinks to `/usr/*` — the jail recreates those symlinks. And
since `/etc/resolv.conf` usually points into `/run` (wiped by the tmpfs), with the
network open the real file is remounted at the symlink's target so DNS keeps
working.

## License

[GPL-3.0](LICENSE), following [ai-jail](https://github.com/akitaonrails/ai-jail).
