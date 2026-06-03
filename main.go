// npm-jail runs npm commands inside a bubblewrap (bwrap) sandbox.
//
// Security model (default, no flags):
//   - $HOME becomes an empty tmpfs (ephemeral): .ssh, .aws, .gnupg, tokens,
//     shell history etc. simply do NOT exist inside the jail.
//   - Only the project directory (cwd) is mounted read-write.
//   - The system (/usr, /etc, /opt) is mounted read-only.
//   - The Node toolchain (node/npm/npx) is mounted read-only.
//   - The npm cache (~/.npm) is mounted read-write to reuse downloads.
//   - ~/.npmrc is mounted read-only, only if it exists.
//   - PID/UTS/IPC/cgroup are isolated in their own namespaces.
//   - Network is shared with the host by default (npm install needs it);
//     use --no-net to isolate the network too.
//
// Per-project config:
//
//	A .npm-jail file (JSON) in the current directory is read automatically.
//	CLI flags take precedence over it. See "npm-jail --init".
//
// Usage:
//
//	npm-jail [npm-jail flags] <npm arguments>
//	npm-jail install express
//	npm-jail --no-net run build
//	npm-jail --hide-env run build
//	npm-jail --rw ./dist ci
//	npm-jail --dry-run install        # only prints the sandbox command line
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// version is injected in release builds by goreleaser (-ldflags). Local builds
// (go build) keep it as "dev".
var version = "dev"

func main() {
	cli, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "npm-jail: "+err.Error())
		os.Exit(2)
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "npm-jail: could not determine the current directory")
		os.Exit(1)
	}

	if cli.doInit {
		if err := writeSampleConfig(cwd); err != nil {
			fmt.Fprintln(os.Stderr, "npm-jail: "+err.Error())
			os.Exit(1)
		}
		fmt.Println("npm-jail: created " + filepath.Join(cwd, configName))
		return
	}

	if len(cli.npmArgs) == 0 {
		fmt.Print(usage)
		return
	}

	cfg, err := resolveConfig(cwd, cli)
	if err != nil {
		fmt.Fprintln(os.Stderr, "npm-jail: "+err.Error())
		os.Exit(1)
	}

	sandbox, err := buildSandboxCommand(cwd, cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "npm-jail: "+err.Error())
		os.Exit(1)
	}

	if cfg.verbose || cfg.dryRun {
		fmt.Fprintln(os.Stderr, sandbox.dryRun)
	}
	if cfg.dryRun {
		return
	}

	cmd := exec.Command(sandbox.program, sandbox.args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			os.Exit(exit.ExitCode())
		}
		fmt.Fprintln(os.Stderr, "npm-jail: failed to execute "+sandbox.program+": "+err.Error())
		os.Exit(1)
	}
}
