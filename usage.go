package main

const usage = `npm-jail - run npm inside an OS sandbox

USAGE:
    npm-jail [npm-jail flags] <npm arguments>

EXAMPLES:
    npm-jail install express
    npm-jail --no-net run build
    npm-jail --hide-env run build
    npm-jail --rw ./out --ro ~/.config/some ci
    npm-jail --dry-run install
    npm-jail --init                    # creates a sample .npm-jail

FLAGS for npm-jail (must come BEFORE npm arguments):
    --no-net           Isolate the network (--unshare-net). npm install that
                       downloads packages will fail; useful for offline builds.
    --net              Force network ON (overrides no_net from .npm-jail).
    --rw PATH          Mount an additional PATH read-write (repeatable).
    --ro PATH          Mount an additional PATH read-only (repeatable).
    --allow-global     Mount the Node toolchain read-write (allows npm i -g).
    --share-home       Do NOT tmpfs $HOME (exposes the real home). Insecure;
                       use only for debugging.
    --hide-env         Hide project .env* files from the npm command. This is
                       automatic for install/ci/add; pass it for run/build.
    --no-hide-env      Do not hide project .env* files, even for install/ci/add.
    --no-config        Ignore the project's .npm-jail file.
    --init             Create a sample .npm-jail in the current directory and exit.
    --verbose, -v      Print the full sandbox command before executing.
    --dry-run          Print the sandbox command and exit without executing.
    --help, -h         Show this help.
    --version          Show the version and exit.

PROJECT .npm-jail FILE (JSON, optional, in the project directory):
    {
      "no_net": false,
      "allow_global": false,
      "rw": ["./out"],
      "ro": ["~/.config/something"]
    }
    Everything after the first token that is not a known flag (or after "--")
    is passed to npm unchanged.
`
