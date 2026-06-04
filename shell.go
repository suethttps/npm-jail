package main

import "strings"

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
