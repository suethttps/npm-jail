package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

var lookPath = exec.LookPath

// resolveNodeToolchain finds the Node toolchain root from the node binary on
// PATH. Returns (toolchain dir, binary dir).
//
// Ex.: /home/u/.local/share/mise/installs/node/25.8.0/bin/node
//
//	-> toolchain = /home/u/.local/share/mise/installs/node/25.8.0
//	-> binDir    = /home/u/.local/share/mise/installs/node/25.8.0/bin
func resolveNodeToolchain() (string, string, error) {
	nodePath, err := lookPath("node")
	if err != nil {
		return "", "", fmt.Errorf("node not found on PATH")
	}
	real, err := filepath.EvalSymlinks(nodePath)
	if err != nil {
		return "", "", fmt.Errorf("could not resolve node path: %w", err)
	}
	// TODO: Validate that npm and npx resolve inside the same toolchain. Some
	// distro layouts split node and npm across different prefixes.
	binDir := filepath.Dir(real)      // .../bin
	toolchain := filepath.Dir(binDir) // .../<version>
	return toolchain, binDir, nil
}

func npmCacheDir(home string) string {
	if c := os.Getenv("npm_config_cache"); c != "" {
		return c
	}
	return filepath.Join(home, ".npm")
}
