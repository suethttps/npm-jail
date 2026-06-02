package main

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestParseArgsStopsAtFirstNpmArg(t *testing.T) {
	cli, err := parseArgs([]string{"--no-net", "--rw", "./out", "install", "--save-dev", "vite"})
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}
	if cli.noNet == nil || !*cli.noNet {
		t.Fatalf("--no-net was not recognized")
	}
	if !reflect.DeepEqual(cli.rw, []string{"./out"}) {
		t.Fatalf("rw = %#v", cli.rw)
	}
	want := []string{"install", "--save-dev", "vite"}
	if !reflect.DeepEqual(cli.npmArgs, want) {
		t.Fatalf("npmArgs = %#v, want %#v", cli.npmArgs, want)
	}
}

func TestParseArgsDoubleDashPassesRestToNpm(t *testing.T) {
	cli, err := parseArgs([]string{"--net", "--", "--version"})
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}
	if cli.noNet == nil || *cli.noNet {
		t.Fatalf("--net should set noNet=false")
	}
	want := []string{"--version"}
	if !reflect.DeepEqual(cli.npmArgs, want) {
		t.Fatalf("npmArgs = %#v, want %#v", cli.npmArgs, want)
	}
}

func TestParseArgsHideEnvFlags(t *testing.T) {
	cli, err := parseArgs([]string{"--hide-env", "run", "build"})
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}
	if cli.hideEnv == nil || !*cli.hideEnv {
		t.Fatalf("--hide-env should set hideEnv=true")
	}

	cli, err = parseArgs([]string{"--no-hide-env", "install"})
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}
	if cli.hideEnv == nil || *cli.hideEnv {
		t.Fatalf("--no-hide-env should set hideEnv=false")
	}
}

func TestResolveConfigMergesFileAndCli(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, configName), `{
  "no_net": true,
  "allow_global": false,
  "rw": ["./file-rw"],
  "ro": ["./file-ro"]
}`)

	bFalse := false
	bTrue := true
	cfg, err := resolveConfig(dir, cliFlags{
		noNet:       &bFalse,
		allowGlobal: &bTrue,
		verbose:     true,
		dryRun:      true,
		rw:          []string{"./cli-rw"},
		ro:          []string{"./cli-ro"},
		npmArgs:     []string{"ci"},
	})
	if err != nil {
		t.Fatalf("resolveConfig returned error: %v", err)
	}
	if cfg.noNet {
		t.Fatalf("CLI --net should override no_net from the file")
	}
	if !cfg.allowGlobal || !cfg.verbose || !cfg.dryRun {
		t.Fatalf("booleans resolved incorrectly: %#v", cfg)
	}
	if !reflect.DeepEqual(cfg.rwExtra, []string{"./file-rw", "./cli-rw"}) {
		t.Fatalf("rwExtra = %#v", cfg.rwExtra)
	}
	if !reflect.DeepEqual(cfg.roExtra, []string{"./file-ro", "./cli-ro"}) {
		t.Fatalf("roExtra = %#v", cfg.roExtra)
	}
	if !reflect.DeepEqual(cfg.npmArgs, []string{"ci"}) {
		t.Fatalf("npmArgs = %#v", cfg.npmArgs)
	}
}

func TestResolveConfigHidesEnvForInstallByDefault(t *testing.T) {
	cfg, err := resolveConfig(t.TempDir(), cliFlags{npmArgs: []string{"install"}})
	if err != nil {
		t.Fatalf("resolveConfig returned error: %v", err)
	}
	if !cfg.hideEnv {
		t.Fatalf("install should hide project .env files by default")
	}

	cfg, err = resolveConfig(t.TempDir(), cliFlags{npmArgs: []string{"run", "dev"}})
	if err != nil {
		t.Fatalf("resolveConfig returned error: %v", err)
	}
	if cfg.hideEnv {
		t.Fatalf("non-install commands should not hide project .env files by default")
	}
}

func TestResolveConfigHideEnvCliOverride(t *testing.T) {
	bTrue := true
	bFalse := false
	cfg, err := resolveConfig(t.TempDir(), cliFlags{hideEnv: &bTrue, npmArgs: []string{"run", "build"}})
	if err != nil {
		t.Fatalf("resolveConfig returned error: %v", err)
	}
	if !cfg.hideEnv {
		t.Fatalf("--hide-env should hide project .env files for non-install commands")
	}

	cfg, err = resolveConfig(t.TempDir(), cliFlags{hideEnv: &bFalse, npmArgs: []string{"install"}})
	if err != nil {
		t.Fatalf("resolveConfig returned error: %v", err)
	}
	if cfg.hideEnv {
		t.Fatalf("--no-hide-env should disable default .env hiding for install")
	}
}

func TestResolveNodeToolchainFollowsActiveNode(t *testing.T) {
	dir := t.TempDir()
	realBin := filepath.Join(dir, "versions", "node", "v22.11.0", "bin")
	shimBin := filepath.Join(dir, "shims")
	if err := os.MkdirAll(realBin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(shimBin, 0o755); err != nil {
		t.Fatal(err)
	}
	realNode := filepath.Join(realBin, "node")
	writeFile(t, realNode, "#!/bin/sh\n")
	if err := os.Chmod(realNode, 0o755); err != nil {
		t.Fatal(err)
	}
	shimNode := filepath.Join(shimBin, "node")
	if err := os.Symlink(realNode, shimNode); err != nil {
		if runtime.GOOS == "windows" {
			t.Skip("symlink unavailable")
		}
		t.Fatal(err)
	}

	restore := stubLookPath(shimNode)
	t.Cleanup(restore)

	toolchain, binDir, err := resolveNodeToolchain()
	if err != nil {
		t.Fatalf("resolveNodeToolchain returned error: %v", err)
	}
	if binDir != realBin {
		t.Fatalf("binDir = %q, want %q", binDir, realBin)
	}
	wantToolchain := filepath.Dir(realBin)
	if toolchain != wantToolchain {
		t.Fatalf("toolchain = %q, want %q", toolchain, wantToolchain)
	}
}

func TestBuildBwrapArgsMountsActiveNodeAndSetsPath(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	cwd := filepath.Join(home, "project")
	nodeBin := filepath.Join(home, ".nvm", "versions", "node", "v22.11.0", "bin")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(nodeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	nodePath := filepath.Join(nodeBin, "node")
	writeFile(t, nodePath, "#!/bin/sh\n")
	if err := os.Chmod(nodePath, 0o755); err != nil {
		t.Fatal(err)
	}

	restoreHome := setEnv(t, "HOME", home)
	t.Cleanup(restoreHome)
	restoreLookPath := stubLookPath(nodePath)
	t.Cleanup(restoreLookPath)

	args, err := buildBwrapArgs(cwd, config{npmArgs: []string{"install"}})
	if err != nil {
		t.Fatalf("buildBwrapArgs returned error: %v", err)
	}
	toolchain := filepath.Dir(nodeBin)
	assertContainsSequence(t, args, "--tmpfs", home)
	assertContainsSequence(t, args, "--ro-bind", toolchain, toolchain)
	assertContainsSequence(t, args, "--bind", cwd, cwd)
	assertContainsSequence(t, args, "--setenv", "PATH", nodeBin+":/usr/bin:/usr/local/bin")
	assertContainsSequence(t, args, "--", "npm", "install")
	if indexOfSequence(args, []string{"--tmpfs", home}) > indexOfSequence(args, []string{"--ro-bind", toolchain, toolchain}) {
		t.Fatalf("HOME tmpfs must come before the toolchain bind")
	}
}

func TestBuildBwrapArgsHonorsAllowGlobal(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	cwd := filepath.Join(home, "project")
	nodeBin := filepath.Join(home, ".local", "share", "mise", "installs", "node", "22.11.0", "bin")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(nodeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	nodePath := filepath.Join(nodeBin, "node")
	writeFile(t, nodePath, "#!/bin/sh\n")

	restoreHome := setEnv(t, "HOME", home)
	t.Cleanup(restoreHome)
	restoreLookPath := stubLookPath(nodePath)
	t.Cleanup(restoreLookPath)

	args, err := buildBwrapArgs(cwd, config{allowGlobal: true, npmArgs: []string{"install"}})
	if err != nil {
		t.Fatalf("buildBwrapArgs returned error: %v", err)
	}
	toolchain := filepath.Dir(nodeBin)
	assertContainsSequence(t, args, "--bind", toolchain, toolchain)
	if containsSequence(args, []string{"--ro-bind", toolchain, toolchain}) {
		t.Fatalf("allowGlobal should not mount the toolchain read-only")
	}
}

func TestBuildBwrapArgsMasksEnvFilesWhenEnabled(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	cwd := filepath.Join(home, "project")
	nodeBin := filepath.Join(home, ".nvm", "versions", "node", "v22.11.0", "bin")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(nodeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	nodePath := filepath.Join(nodeBin, "node")
	writeFile(t, nodePath, "#!/bin/sh\n")
	writeFile(t, filepath.Join(cwd, ".env"), "SECRET=1\n")
	writeFile(t, filepath.Join(cwd, ".env.local"), "SECRET=2\n")
	if err := os.Mkdir(filepath.Join(cwd, ".env.d"), 0o755); err != nil {
		t.Fatal(err)
	}

	restoreHome := setEnv(t, "HOME", home)
	t.Cleanup(restoreHome)
	restoreLookPath := stubLookPath(nodePath)
	t.Cleanup(restoreLookPath)

	args, err := buildBwrapArgs(cwd, config{hideEnv: true, npmArgs: []string{"install"}})
	if err != nil {
		t.Fatalf("buildBwrapArgs returned error: %v", err)
	}
	assertContainsSequence(t, args, "--ro-bind", "/dev/null", filepath.Join(cwd, ".env"))
	assertContainsSequence(t, args, "--ro-bind", "/dev/null", filepath.Join(cwd, ".env.local"))
	if containsSequence(args, []string{"--ro-bind", "/dev/null", filepath.Join(cwd, ".env.d")}) {
		t.Fatalf("env mask should skip directories")
	}
	if indexOfSequence(args, []string{"--bind", cwd, cwd}) > indexOfSequence(args, []string{"--ro-bind", "/dev/null", filepath.Join(cwd, ".env")}) {
		t.Fatalf("env masks should come after the project bind")
	}
}

func TestBuildBwrapArgsDoesNotMaskEnvFilesWhenDisabled(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	cwd := filepath.Join(home, "project")
	nodeBin := filepath.Join(home, ".nvm", "versions", "node", "v22.11.0", "bin")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(nodeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	nodePath := filepath.Join(nodeBin, "node")
	writeFile(t, nodePath, "#!/bin/sh\n")
	writeFile(t, filepath.Join(cwd, ".env"), "SECRET=1\n")

	restoreHome := setEnv(t, "HOME", home)
	t.Cleanup(restoreHome)
	restoreLookPath := stubLookPath(nodePath)
	t.Cleanup(restoreLookPath)

	args, err := buildBwrapArgs(cwd, config{npmArgs: []string{"run", "dev"}})
	if err != nil {
		t.Fatalf("buildBwrapArgs returned error: %v", err)
	}
	if containsSequence(args, []string{"--ro-bind", "/dev/null", filepath.Join(cwd, ".env")}) {
		t.Fatalf("env mask should not be added when hideEnv is false")
	}
}

func TestBuildSandboxExecArgsGeneratesMacOSProfile(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	cwd := filepath.Join(home, "project")
	nodeBin := filepath.Join(home, ".nvm", "versions", "node", "v22.11.0", "bin")
	for _, p := range []string{cwd, nodeBin, filepath.Join(home, ".ssh"), filepath.Join(home, ".npm")} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	nodePath := filepath.Join(nodeBin, "node")
	writeFile(t, nodePath, "#!/bin/sh\n")
	writeFile(t, filepath.Join(cwd, ".env"), "SECRET=1\n")

	restoreHome := setEnv(t, "HOME", home)
	t.Cleanup(restoreHome)
	restoreLookPath := stubLookPath(nodePath)
	t.Cleanup(restoreLookPath)

	args, profile, err := buildSandboxExecArgs(cwd, config{hideEnv: true, npmArgs: []string{"install"}})
	if err != nil {
		t.Fatalf("buildSandboxExecArgs returned error: %v", err)
	}
	assertContainsSequence(t, args, "-p", profile, "--", "/usr/bin/env")
	if !strings.Contains(profile, "(deny default)") {
		t.Fatalf("profile should deny by default:\n%s", profile)
	}
	if !strings.Contains(profile, "(allow network-outbound)") {
		t.Fatalf("network should be allowed by default:\n%s", profile)
	}
	if !strings.Contains(profile, `(deny file-read* (subpath "`+filepath.Join(home, ".ssh")+`"))`) {
		t.Fatalf("profile should deny .ssh reads:\n%s", profile)
	}
	if !strings.Contains(profile, `(deny file-read* (literal "`+filepath.Join(cwd, ".env")+`"))`) {
		t.Fatalf("profile should deny .env reads when hideEnv is enabled:\n%s", profile)
	}
	if !strings.Contains(profile, `(allow file-write* (subpath "`+cwd+`"))`) {
		t.Fatalf("profile should allow project writes:\n%s", profile)
	}
}

func TestBuildMacOSProfileNoNetOmitsNetwork(t *testing.T) {
	profile := buildMacOSSandboxProfile("/tmp/project", "/tmp/home", "/tmp/node", config{noNet: true})
	if strings.Contains(profile, "network-outbound") || strings.Contains(profile, "network-inbound") {
		t.Fatalf("--no-net should omit network rules:\n%s", profile)
	}
}

func TestSBPLEscape(t *testing.T) {
	got := sbplEscape("/tmp/with\"quote\\slash")
	want := `/tmp/with\"quote\\slash`
	if got != want {
		t.Fatalf("sbplEscape = %q, want %q", got, want)
	}
}

func TestNpmCacheDirUsesEnvOverride(t *testing.T) {
	restore := setEnv(t, "npm_config_cache", "/tmp/npm-cache")
	t.Cleanup(restore)
	if got := npmCacheDir("/home/user"); got != "/tmp/npm-cache" {
		t.Fatalf("npmCacheDir = %q", got)
	}
}

func TestMustAbsExpandsHome(t *testing.T) {
	got := mustAbs("/home/user", "~/cache")
	if got != filepath.Join("/home/user", "cache") {
		t.Fatalf("mustAbs = %q", got)
	}
}

func TestShellQuote(t *testing.T) {
	got := shellQuote([]string{"--bind", "/path with space", "it's"})
	want := "--bind '/path with space' 'it'\\''s'"
	if got != want {
		t.Fatalf("shellQuote = %q, want %q", got, want)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func stubLookPath(nodePath string) func() {
	old := lookPath
	lookPath = func(file string) (string, error) {
		if file != "node" {
			return "", os.ErrNotExist
		}
		return nodePath, nil
	}
	return func() { lookPath = old }
}

func setEnv(t *testing.T, key, value string) func() {
	t.Helper()
	old, ok := os.LookupEnv(key)
	if err := os.Setenv(key, value); err != nil {
		t.Fatal(err)
	}
	return func() {
		if ok {
			_ = os.Setenv(key, old)
		} else {
			_ = os.Unsetenv(key)
		}
	}
}

func assertContainsSequence(t *testing.T, args []string, seq ...string) {
	t.Helper()
	if !containsSequence(args, seq) {
		t.Fatalf("args do not contain sequence %s\nargs: %s", strings.Join(seq, " "), strings.Join(args, " "))
	}
}

func containsSequence(args, seq []string) bool {
	return indexOfSequence(args, seq) >= 0
}

func indexOfSequence(args, seq []string) int {
	if len(seq) == 0 || len(seq) > len(args) {
		return -1
	}
	for i := 0; i <= len(args)-len(seq); i++ {
		if reflect.DeepEqual(args[i:i+len(seq)], seq) {
			return i
		}
	}
	return -1
}
