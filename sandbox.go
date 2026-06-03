package main

import (
	"fmt"
	"runtime"
)

type sandboxCommand struct {
	program string
	args    []string
	dryRun  string
}

func buildSandboxCommand(cwd string, cfg config) (sandboxCommand, error) {
	switch runtime.GOOS {
	case "linux":
		args, err := buildBwrapArgs(cwd, cfg)
		if err != nil {
			return sandboxCommand{}, err
		}
		return sandboxCommand{
			program: "bwrap",
			args:    args,
			dryRun:  "bwrap " + shellQuote(args),
		}, nil
	case "darwin":
		args, profile, err := buildSandboxExecArgs(cwd, cfg)
		if err != nil {
			return sandboxCommand{}, err
		}
		return sandboxCommand{
			program: "/usr/bin/sandbox-exec",
			args:    args,
			dryRun:  formatSandboxExecDryRun(args, profile),
		}, nil
	default:
		return sandboxCommand{}, fmt.Errorf("unsupported OS %q: npm-jail supports Linux and macOS", runtime.GOOS)
	}
}
