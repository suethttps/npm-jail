package main

import (
	"fmt"
	"os"
)

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
