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
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const configName = ".npm-jail"

// version is injected in release builds by goreleaser (-ldflags). Local builds
// (go build) keep it as "dev".
var version = "dev"

var lookPath = exec.LookPath

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

// config is the final resolved state (file + CLI).
type config struct {
	noNet       bool
	allowGlobal bool
	shareHome   bool
	hideEnv     bool
	verbose     bool
	dryRun      bool
	rwExtra     []string
	roExtra     []string
	npmArgs     []string
}

type sandboxCommand struct {
	program string
	args    []string
	dryRun  string
}

// fileConfig is the .npm-jail file format.
type fileConfig struct {
	NoNet       bool     `json:"no_net"`
	AllowGlobal bool     `json:"allow_global"`
	RW          []string `json:"rw"`
	RO          []string `json:"ro"`
}

// cliFlags stores what came from the command line. Booleans are pointers to
// distinguish "not provided" from "provided as false", allowing the CLI to
// override the file predictably.
type cliFlags struct {
	noNet       *bool
	allowGlobal *bool
	shareHome   *bool
	hideEnv     *bool
	verbose     bool
	dryRun      bool
	noConfig    bool
	doInit      bool
	rw          []string
	ro          []string
	npmArgs     []string
}

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

func parseArgs(in []string) (cliFlags, error) {
	var c cliFlags
	bTrue, bFalse := true, false
	for i := 0; i < len(in); i++ {
		a := in[i]
		switch a {
		case "--help", "-h":
			// TODO: Return a parse result for help/version instead of calling os.Exit
			// inside parseArgs, so CLI behavior can be tested without subprocesses.
			fmt.Print(usage)
			os.Exit(0)
		case "--version":
			fmt.Println("npm-jail " + version)
			os.Exit(0)
		case "--init":
			c.doInit = true
		case "--no-config":
			c.noConfig = true
		case "--no-net":
			c.noNet = &bTrue
		case "--net":
			c.noNet = &bFalse
		case "--allow-global":
			c.allowGlobal = &bTrue
		case "--share-home":
			c.shareHome = &bTrue
		case "--hide-env":
			c.hideEnv = &bTrue
		case "--no-hide-env":
			c.hideEnv = &bFalse
		case "--verbose", "-v":
			c.verbose = true
		case "--dry-run":
			c.dryRun = true
		case "--rw":
			i++
			if i >= len(in) {
				return c, fmt.Errorf("--rw requires a PATH")
			}
			c.rw = append(c.rw, in[i])
		case "--ro":
			i++
			if i >= len(in) {
				return c, fmt.Errorf("--ro requires a PATH")
			}
			c.ro = append(c.ro, in[i])
		case "--":
			c.npmArgs = append(c.npmArgs, in[i+1:]...)
			return c, nil
		default:
			// First unknown token: everything from here on belongs to npm.
			c.npmArgs = append(c.npmArgs, in[i:]...)
			return c, nil
		}
	}
	return c, nil
}

// resolveConfig merges the .npm-jail file (defaults) with CLI flags
// (precedence). rw/ro lists are merged; CLI booleans override.
func resolveConfig(cwd string, cli cliFlags) (config, error) {
	var fc fileConfig
	if !cli.noConfig {
		loaded, err := loadConfig(cwd)
		if err != nil {
			return config{}, err
		}
		if loaded != nil {
			fc = *loaded
		}
	}

	cfg := config{
		noNet:       fc.NoNet,
		allowGlobal: fc.AllowGlobal,
		hideEnv:     isNpmInstallCommand(cli.npmArgs),
		verbose:     cli.verbose,
		dryRun:      cli.dryRun,
		rwExtra:     append(append([]string{}, fc.RW...), cli.rw...),
		roExtra:     append(append([]string{}, fc.RO...), cli.ro...),
		npmArgs:     cli.npmArgs,
	}
	if cli.noNet != nil {
		cfg.noNet = *cli.noNet
	}
	if cli.allowGlobal != nil {
		cfg.allowGlobal = *cli.allowGlobal
	}
	if cli.shareHome != nil {
		cfg.shareHome = *cli.shareHome
	}
	if cli.hideEnv != nil {
		cfg.hideEnv = *cli.hideEnv
	}
	return cfg, nil
}

// loadConfig reads .npm-jail from the project directory. Returns nil if missing.
func loadConfig(cwd string) (*fileConfig, error) {
	path := filepath.Join(cwd, configName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("could not read %s: %w", configName, err)
	}
	var fc fileConfig
	if err := json.Unmarshal(data, &fc); err != nil {
		return nil, fmt.Errorf("%s is invalid JSON: %w", configName, err)
	}
	return &fc, nil
}

func writeSampleConfig(cwd string) error {
	path := filepath.Join(cwd, configName)
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists", configName)
	}
	sample := fileConfig{
		NoNet:       false,
		AllowGlobal: false,
		RW:          []string{},
		RO:          []string{},
	}
	data, err := json.MarshalIndent(sample, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
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

func formatSandboxExecDryRun(args []string, profile string) string {
	shown := append([]string{}, args...)
	if len(shown) >= 2 && shown[0] == "-p" {
		shown[1] = "<profile>"
	}
	return "sandbox-exec " + shellQuote(shown) + "\n\n# SBPL profile:\n" + profile
}

func buildBwrapArgs(cwd string, cfg config) ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil, fmt.Errorf("could not determine $HOME")
	}

	toolchain, binDir, err := resolveNodeToolchain()
	if err != nil {
		return nil, err
	}

	var a []string
	add := func(xs ...string) { a = append(a, xs...) }

	// Namespace isolation and basic security.
	add("--die-with-parent")
	add("--unshare-pid", "--unshare-uts", "--unshare-ipc", "--unshare-cgroup-try")
	if cfg.noNet {
		add("--unshare-net")
	}

	// System root, read-only. /usr + usr-merge symlink recreation.
	// TODO: Consider masking stable machine identifiers such as /etc/machine-id
	// and /var/lib/dbus/machine-id to reduce fingerprinting from lifecycle scripts.
	add("--ro-bind", "/usr", "/usr")
	for _, link := range []string{"/bin", "/sbin", "/lib", "/lib64", "/lib32"} {
		addRootEntry(&a, link)
	}
	for _, dir := range []string{"/etc", "/opt"} {
		if isDir(dir) {
			add("--ro-bind", dir, dir)
		}
	}

	// Pseudo-filesystems and ephemeral dirs.
	// TODO: Replace the broad /dev bind with a smaller device allowlist if Node/npm
	// do not need full host device visibility.
	add("--proc", "/proc")
	add("--dev", "/dev")
	add("--tmpfs", "/tmp")
	add("--tmpfs", "/run")

	// With shared network we need a working resolv.conf.
	if !cfg.noNet {
		addResolvConf(&a)
	}

	// Ephemeral $HOME (hides everything), then remount only what is needed.
	if !cfg.shareHome {
		add("--tmpfs", home)
	}

	// Node toolchain (node/npm/npx). Comes AFTER the home tmpfs because it often
	// lives under $HOME (for example, mise).
	// TODO: Special-case system Node paths such as /usr/bin/node. With
	// --allow-global, toolchain=/usr would remount /usr read-write, which is much
	// broader than intended.
	if cfg.allowGlobal {
		add("--bind", toolchain, toolchain)
	} else {
		add("--ro-bind", toolchain, toolchain)
	}

	// npm cache (read-write) and .npmrc (read-only, if it exists).
	// TODO: Normalize npm_config_cache through mustAbs before binding it; npm
	// accepts relative cache paths, but bwrap binds should be explicit host paths.
	cache := npmCacheDir(home)
	add("--bind-try", cache, cache)
	npmrc := filepath.Join(home, ".npmrc")
	if fileExists(npmrc) {
		add("--ro-bind", npmrc, npmrc)
	}

	// Project directory: read-write.
	add("--bind", cwd, cwd)

	// Extra mounts requested by the user (file + CLI).
	for _, p := range cfg.roExtra {
		abs := mustAbs(home, p)
		add("--ro-bind-try", abs, abs)
	}
	for _, p := range cfg.rwExtra {
		abs := mustAbs(home, p)
		add("--bind-try", abs, abs)
	}
	if cfg.hideEnv {
		addEnvMasks(&a, cwd)
	}

	// Environment: inherit from host, but pin HOME, PATH and cwd.
	// TODO: Add an optional env allowlist/clearenv mode so shell secrets exposed in
	// environment variables are not inherited by lifecycle scripts by default.
	path := binDir + ":/usr/bin:/usr/local/bin"
	add("--setenv", "HOME", home)
	add("--setenv", "PATH", path)
	add("--chdir", cwd)

	// Final command.
	add("--", "npm")
	add(cfg.npmArgs...)
	return a, nil
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

// addResolvConf ensures DNS inside the jail when the network is shared.
//
// On distros with systemd-resolved, /etc/resolv.conf is a symlink to something
// under /run (which we replace with tmpfs). Since /etc is read-only, we cannot
// create a file there; instead, bind the real file at the symlink TARGET, which
// lands under /run (writable tmpfs), making the symlink resolve again. If
// resolv.conf is a regular file, the read-only /etc bind already covers it.
func addResolvConf(a *[]string) {
	real, err := filepath.EvalSymlinks("/etc/resolv.conf")
	if err != nil {
		// TODO: Surface this in verbose mode; silent DNS setup failures make network
		// issues inside the jail harder to diagnose.
		return
	}
	fi, err := os.Lstat("/etc/resolv.conf")
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		return // regular file: already included by --ro-bind /etc
	}
	target, err := os.Readlink("/etc/resolv.conf")
	if err != nil {
		return
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join("/etc", target)
	}
	*a = append(*a, "--ro-bind", real, target)
}

// addRootEntry replicates a root entry (/bin, /lib, ...): if it is a symlink
// (usr-merge layout), recreate the symlink; if it is a real directory, mount it
// read-only.
func addRootEntry(a *[]string, p string) {
	fi, err := os.Lstat(p)
	if err != nil {
		return // does not exist on this distro
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(p)
		if err == nil {
			*a = append(*a, "--symlink", target, p)
		}
		return
	}
	if fi.IsDir() {
		*a = append(*a, "--ro-bind", p, p)
	}
}

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

// shellQuote is only used to print the bwrap command line readably.
func shellQuote(args []string) string {
	var b strings.Builder
	for i, s := range args {
		if i > 0 {
			b.WriteByte(' ')
		}
		if s == "" || strings.ContainsAny(s, " \t\n\"'\\$") {
			b.WriteString("'" + strings.ReplaceAll(s, "'", `'\''`) + "'")
		} else {
			b.WriteString(s)
		}
	}
	return b.String()
}
