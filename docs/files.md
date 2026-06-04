# File Responsibilities

This project keeps all Go code in `package main`. The split is by responsibility,
not by public API boundaries.

## Go Files

- `main.go`: CLI entrypoint. Parses arguments, resolves config, builds the sandbox command, prints dry-run output, and executes the sandbox process.
- `usage.go`: User-facing help text printed by `--help` or when no npm arguments are provided.
- `cli.go`: npm-jail flag parsing. Separates npm-jail flags from the npm arguments that must be passed through unchanged.
- `config.go`: `.npm-jail` JSON config loading, sample config creation, and file-plus-CLI config resolution.
- `sandbox.go`: Platform dispatcher. Chooses Linux `bwrap` or macOS `sandbox-exec` and returns the executable command metadata.
- `bwrap.go`: Linux sandbox builder. Assembles the `bwrap` arguments, mounts, namespaces, DNS handling, root entry handling, and final npm command.
- `sandboxexec.go`: macOS sandbox builder. Assembles `sandbox-exec` arguments and generates the SBPL profile used by Apple's sandbox.
- `node.go`: Node toolchain discovery and npm cache path resolution.
- `paths.go`: Shared path helpers, `.env*` masking, install-command detection, and filesystem existence checks.
- `shell.go`: Shell quoting used only for readable dry-run command output.
- `main_test.go`: Unit tests for argument parsing, config merging, sandbox argument generation, macOS profile generation, and shared helpers.

## Non-Go Files

- `README.md`: User-facing project documentation and examples.
- `CLAUDE.md`: Contributor guidance for AI-assisted work in this repository.
- `go.mod`: Go module definition. The project intentionally uses only the standard library.
- `mise.toml`: Tool version setup used to run Go commands in this repository.
- `.goreleaser.yaml`: Release build configuration for Linux and macOS binaries.
- `.github/workflows/`: CI, release, and auto-release automation.
- `.github/scripts/`: Release support scripts, including AUR publishing.
- `dist/`: Local GoReleaser output. Generated artifacts should not be treated as source code.
