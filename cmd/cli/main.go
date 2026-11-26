// Command duet is the command line client of duet.
//
// Subcommands live in their own files and register themselves in init().
package main

import (
	"fmt"
	"os"
	"sort"
)

type command struct {
	summary string
	run     func(args []string) error
}

var commands = map[string]command{}

func register(name, summary string, run func(args []string) error) {
	if _, dup := commands[name]; dup {
		panic("duplicate command " + name)
	}
	commands[name] = command{summary, run}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: duet <command> [args]")
	names := make([]string, 0, len(commands))
	for n := range commands {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintf(os.Stderr, "  %-12s %s\n", n, commands[n].summary)
	}
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	c, ok := commands[os.Args[1]]
	if !ok {
		usage()
		os.Exit(2)
	}
	if err := c.run(os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "duet:", err)
		os.Exit(1)
	}
}
