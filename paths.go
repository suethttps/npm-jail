package main

import (
	"os"
	"path/filepath"
	"strings"
)

func existingPaths(paths []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range paths {
		if p == "" || !pathExists(p) {
			continue
		}
		canonical := canonicalizeOrKeep(p)
		if seen[canonical] {
			continue
		}
		seen[canonical] = true
		out = append(out, canonical)
	}
	return out
}

func canonicalizeOrKeep(p string) string {
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return real
	}
	return p
}

func isNpmInstallCommand(args []string) bool {
	for _, arg := range args {
		if arg == "" || strings.HasPrefix(arg, "-") {
			continue
		}
		switch arg {
		case "install", "i", "ci", "add":
			return true
		default:
			return false
		}
	}
	return false
}

func addEnvMasks(a *[]string, cwd string) {
	for _, p := range envMaskPaths(cwd) {
		*a = append(*a, "--ro-bind", "/dev/null", p)
	}
}

func envMaskPaths(cwd string) []string {
	matches, err := filepath.Glob(filepath.Join(cwd, ".env*"))
	if err != nil {
		return nil
	}
	var paths []string
	for _, p := range matches {
		fi, err := os.Lstat(p)
		if err != nil || fi.IsDir() {
			continue
		}
		paths = append(paths, p)
	}
	return paths
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func pathExists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func mustAbs(home, p string) string {
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		p = filepath.Join(home, p[2:])
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}
