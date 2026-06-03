package main

import (
	"fmt"
	"os"
	"path/filepath"
)

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
