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

- `bwrap` (bubblewrap) — `pacman -S bubblewrap`
- `node`/`npm` on `PATH` (tested with Node via [mise](https://mise.jdx.dev/))
- Go 1.26+ only to build

## Build

```bash
go build -o npm-jail .
# optional: put it on PATH
install -Dm755 npm-jail ~/.local/bin/npm-jail
```

(In this repository Go is provided by `mise`; run `mise exec -- go build -o npm-jail .`.)

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

## How it works

`npm-jail` doesn't call npm directly: it assembles the `bwrap` argument list, sets
up the isolated filesystem, and then runs `npm <args>` inside it. See exactly what
gets mounted with `--dry-run`.

Portability detail: on *usr-merge* distros (Arch, Fedora…), `/bin`, `/sbin`,
`/lib`, `/lib64` are symlinks to `/usr/*` — the jail recreates those symlinks. And
since `/etc/resolv.conf` usually points into `/run` (wiped by the tmpfs), with the
network open the real file is remounted at the symlink's target so DNS keeps
working.
