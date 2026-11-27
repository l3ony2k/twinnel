// Command twinnel is the command line client of Twinnel.
//
// Subcommands live in their own files and register themselves in init().
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/l3ony2k/twinnel/core/legacy"
	"github.com/l3ony2k/twinnel/core/rpc"
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
	fmt.Fprintln(os.Stderr, "usage: twinnel <command> [args]")
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
	if n := legacy.EnvNotice(legacy.ApplyEnvAliases()); n != "" {
		fmt.Fprintln(os.Stderr, "twinnel:", n)
	}
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch a := os.Args[1]; a {
	case "--version", "-version", "-v":
		os.Args[1] = "version"
	case "help", "-h", "-help", "--help":
		usage()
		return
	}
	c, ok := commands[os.Args[1]]
	if !ok {
		fmt.Fprintf(os.Stderr, "twinnel: unknown command %q\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err := c.run(os.Args[2:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return // -h: the flag set printed the usage
		}
		fmt.Fprintln(os.Stderr, "twinnel:", errorText(err))
		os.Exit(1)
	}
}

// errorText is err for people: a helper error reads as its message, not
// "rpc error -32003: …" (the JSON-RPC code says nothing to them; --json
// output and MCP keep it).
func errorText(err error) string {
	msg := err.Error()
	var re *rpc.Error
	if errors.As(err, &re) && re.Message != "" {
		msg = strings.Replace(msg, re.Error(), re.Message, 1)
	}
	return msg
}
