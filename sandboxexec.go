package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func formatSandboxExecDryRun(args []string, profile string) string {
	shown := append([]string{}, args...)
	if len(shown) >= 2 && shown[0] == "-p" {
		shown[1] = "<profile>"
	}
	return "sandbox-exec " + shellQuote(shown) + "\n\n# SBPL profile:\n" + profile
}

func buildSandboxExecArgs(cwd string, cfg config) ([]string, string, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil, "", fmt.Errorf("could not determine $HOME")
	}

	toolchain, binDir, err := resolveNodeToolchain()
	if err != nil {
		return nil, "", err
	}

	profile := buildMacOSSandboxProfile(cwd, home, toolchain, cfg)
	path := binDir + ":/usr/bin:/usr/local/bin:/opt/homebrew/bin"
	args := []string{"-p", profile, "--", "/usr/bin/env", "HOME=" + home, "PATH=" + path, "npm"}
	args = append(args, cfg.npmArgs...)
	return args, profile, nil
}

func buildMacOSSandboxProfile(cwd, home, toolchain string, cfg config) string {
	var b strings.Builder
	b.WriteString("(version 1)\n")
	b.WriteString("(deny default)\n\n")

	b.WriteString("; Process operations\n")
	b.WriteString("(allow process-exec)\n")
	b.WriteString("(allow process-fork)\n")
	b.WriteString("(allow process-info* (target same-sandbox))\n")
	b.WriteString("(allow signal)\n")
	b.WriteString("(allow sysctl-read)\n\n")

	b.WriteString("; IPC, terminals and devices\n")
	b.WriteString("(allow mach-lookup)\n")
	b.WriteString("(allow mach-register)\n")
	b.WriteString("(allow mach-host*)\n")
	b.WriteString("(allow ipc-posix-shm-read-data)\n")
	b.WriteString("(allow ipc-posix-shm-write-data)\n")
	b.WriteString("(allow ipc-posix-shm-read-metadata)\n")
	b.WriteString("(allow ipc-posix-shm-write-create)\n")
	b.WriteString("(allow ipc-posix-sem)\n")
	b.WriteString("(allow pseudo-tty)\n")
	b.WriteString("(allow file-ioctl)\n")
	b.WriteString("(allow file-read* file-write* (literal \"/dev/ptmx\"))\n")
	b.WriteString("(allow file-read* file-write* (regex #\"^/dev/ttys[0-9]+\"))\n")
	b.WriteString("(allow file-read* file-write* (literal \"/dev/null\"))\n")
	b.WriteString("(allow file-read* file-write* (literal \"/dev/zero\"))\n")
	b.WriteString("(allow file-read* (literal \"/dev/random\"))\n")
	b.WriteString("(allow file-read* (literal \"/dev/urandom\"))\n")
	b.WriteString("(allow iokit-open)\n\n")

	if !cfg.noNet {
		b.WriteString("; Network\n")
		b.WriteString("(allow network-outbound)\n")
		b.WriteString("(allow network-inbound)\n")
		b.WriteString("(allow network-bind)\n")
		b.WriteString("(allow system-socket)\n\n")
	}

	b.WriteString("; File reads: broadly allowed, with sensitive paths denied below\n")
	b.WriteString("(allow file-read*)\n")
	for _, p := range macOSSensitiveReadDenyPaths(home, cwd, cfg) {
		writeSBPLPathRule(&b, "deny", "file-read*", p)
	}
	b.WriteByte('\n')

	b.WriteString("; File writes: allow only project, npm cache, temp, and explicit rw paths\n")
	for _, p := range macOSWritablePaths(cwd, home, toolchain, cfg) {
		writeSBPLPathRule(&b, "allow", "file-write*", p)
	}
	return b.String()
}

func macOSSensitiveReadDenyPaths(home, cwd string, cfg config) []string {
	if cfg.shareHome {
		return nil
	}
	paths := []string{
		filepath.Join(home, ".ssh"),
		filepath.Join(home, ".aws"),
		filepath.Join(home, ".gnupg"),
		filepath.Join(home, ".netrc"),
		filepath.Join(home, ".bash_history"),
		filepath.Join(home, ".zsh_history"),
		filepath.Join(home, "Library", "Mail"),
		filepath.Join(home, "Library", "Messages"),
		filepath.Join(home, "Library", "Safari"),
		filepath.Join(home, "Library", "Cookies"),
	}
	if cfg.hideEnv {
		paths = append(paths, envMaskPaths(cwd)...)
	}
	return existingPaths(paths)
}

func macOSWritablePaths(cwd, home, toolchain string, cfg config) []string {
	paths := []string{
		cwd,
		npmCacheDir(home),
		"/tmp",
		"/private/tmp",
		"/private/var/tmp",
		"/private/var/folders",
	}
	if tmpdir := os.Getenv("TMPDIR"); tmpdir != "" {
		paths = append(paths, tmpdir)
	}
	if cfg.shareHome {
		paths = append(paths, home)
	}
	if cfg.allowGlobal {
		paths = append(paths, toolchain)
	}
	for _, p := range cfg.rwExtra {
		paths = append(paths, mustAbs(home, p))
	}
	return existingPaths(paths)
}

func writeSBPLPathRule(b *strings.Builder, action, op, path string) {
	kind := "literal"
	if isDir(path) {
		kind = "subpath"
	}
	b.WriteString("(")
	b.WriteString(action)
	b.WriteByte(' ')
	b.WriteString(op)
	b.WriteString(" (")
	b.WriteString(kind)
	b.WriteString(" \"")
	b.WriteString(sbplEscape(path))
	b.WriteString("\"))\n")
}

func sbplEscape(s string) string {
	var b strings.Builder
	for _, c := range s {
		switch c {
		case '\\':
			b.WriteString("\\\\")
		case '"':
			b.WriteString("\\\"")
		case '\n':
			b.WriteString("\\n")
		case '\r':
			b.WriteString("\\r")
		case '\t':
			b.WriteString("\\t")
		default:
			b.WriteRune(c)
		}
	}
	return b.String()
}
