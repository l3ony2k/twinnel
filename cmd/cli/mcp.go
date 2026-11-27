package main

// twinnel mcp: the MCP server for local AI agents (core/agent), on stdio.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/l3ony2k/twinnel/core/agent"
	"github.com/l3ony2k/twinnel/core/health"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/status"
	"github.com/l3ony2k/twinnel/core/tailscale"
	coreversion "github.com/l3ony2k/twinnel/core/version"
)

func init() {
	register("mcp", "serve Twinnel to local AI agents over MCP (stdio); see docs/agents.md", runMCP)
}

// mcpUsage is printed for -h. stdout belongs to the protocol, so nothing
// else is ever printed there.
const mcpUsage = `usage: twinnel mcp [--list-tools] [--json]

Serves Twinnel's tools to an MCP client over stdin/stdout. Add it to a client:

  claude mcp add twinnel -- twinnel mcp
  codex mcp add twinnel -- twinnel mcp

The person turns agent access and its tiers on in Twinnel → 设置 → 本地 Agent.
--list-tools prints the tools and their tiers and exits.

`

func runMCP(args []string) error {
	fs, cf := newFlagSet("mcp", mcpUsage)
	list := fs.Bool("list-tools", false, "print the tools and their tiers, then exit")
	if _, done, err := parse(fs, args); done || err != nil {
		return err
	}
	srv := agent.New(mcpDeps(cf.socket))
	if *list {
		tools := srv.Tools()
		if cf.json {
			b, _ := json.MarshalIndent(tools, "", "  ")
			fmt.Println(string(b))
			return nil
		}
		w := tw()
		for _, t := range tools {
			fmt.Fprintf(w, "%s\t%s\t%s\n", t.Name, t.Tier, t.Title)
		}
		return w.Flush()
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return srv.Run(ctx, agent.NewStdioTransport(os.Stdin, os.Stdout, os.Stderr))
}

// mcpDeps wires the server to the helper on socket, the controller
// (TWINNEL_CONTROLLER pins it, for development), Tailscale's LocalAPI (or
// TWINNEL_TAILSCALE_STATUS_FILE) and the local doctor.
func mcpDeps(socket string) agent.Deps {
	d := agent.Deps{
		Dial: func(ctx context.Context) (agent.Caller, func(), error) {
			ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			c, err := rpc.Dial(ctx, nil, socket)
			if err != nil {
				return nil, nil, err
			}
			return c, func() { c.Close() }, nil
		},
		Controller: os.Getenv(status.EnvController),
		Secret:     os.Getenv(status.EnvSecret),
		Tailscale:  tailscale.DefaultAPI(),
		ReadOnly:   os.Getenv(status.EnvReadOnly) == "1",
		Version:    coreversion.Version,
	}
	d.HelperBinaries = func(ctx context.Context, running string) *coreversion.HelperBinaries {
		return collectHelperBinaries(ctx, 5*time.Second, running)
	}
	d.Doctor = func(ctx context.Context) (json.RawMessage, error) {
		// The same run as `twinnel doctor`: the helper's settings and controller.
		settings, _ := healthSettings(ctx, socket)
		env, err := localHealthEnv(ctx, socket, "", "", &settings)
		if err != nil {
			return nil, err
		}
		runner := &health.Runner{Registry: health.DefaultRegistry(), Timeout: 15 * time.Second}
		rep := runner.Run(ctx, env)
		b, err := rep.JSON()
		return json.RawMessage(b), err
	}
	return d
}
