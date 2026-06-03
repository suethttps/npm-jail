package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const configName = ".npm-jail"

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

// fileConfig is the .npm-jail file format.
type fileConfig struct {
	NoNet       bool     `json:"no_net"`
	AllowGlobal bool     `json:"allow_global"`
	RW          []string `json:"rw"`
	RO          []string `json:"ro"`
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
