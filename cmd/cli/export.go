package main

// twinnel export: the zip the app's 停止使用 → 导出配置 saves (core/setup.
// ExportConfig, built by the helper's config.export). `twinnel import`
// reads it on another machine.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/setup"
)

func init() {
	register("export", "save the running config, subscriptions and overrides as a zip for 'twinnel import'", runExport)
}

const exportUsage = `usage: twinnel export [--out FILE|-] [--no-subscriptions] [--force] [--json]

Saves the running config.yaml (without the controller secret), your
subscriptions and the overrides you wrote as a zip: the same file as the
app's 停止使用 → 导出配置. Bring it to another machine with
'twinnel import FILE'; mihomo can also use its config.yaml without Twinnel.

The file holds subscription URLs and node passwords: keep it private.
--out - writes the zip to stdout (not to a terminal).

`

// exportSecretsWarning is printed with every export: config.yaml always
// carries the nodes, and subscriptions their URLs.
const exportSecretsWarning = "warning: the file holds subscription URLs and node passwords: keep it private, delete it when done"

// ExportResult is `twinnel export --json`'s output. The zip is never in it.
type ExportResult struct {
	Path          string   `json:"path"`
	Bytes         int      `json:"bytes"`
	Layers        int      `json:"layers"`
	Subscriptions int      `json:"subscriptions"`
	NoContent     bool     `json:"no_content,omitempty"`
	Warnings      []string `json:"warnings"`
}

func runExport(args []string) error {
	fset, cf := newFlagSet("export", exportUsage)
	out := fset.String("out", "", "write the zip to `FILE` (default: "+setup.ExportFileName(time.Now())+" here; - for stdout)")
	noSubs := fset.Bool("no-subscriptions", false, "leave the subscriptions and their URLs out")
	force := fset.Bool("force", false, "replace FILE when it exists")
	pos, done, err := parse(fset, args)
	if done || err != nil {
		return err
	}
	if len(pos) > 0 {
		fset.Usage()
		return fmt.Errorf("unexpected argument %q: the file goes in --out", pos[0])
	}
	path := *out
	if path == "" {
		path = setup.ExportFileName(time.Now())
	}
	toStdout := path == "-"
	switch {
	case toStdout && cf.json:
		return errors.New("--out - and --json are exclusive: both write to stdout")
	case toStdout && isTerminal(os.Stdout) && stdout == os.Stdout:
		return errors.New("will not write a zip to the terminal: pass --out FILE, or redirect stdout")
	case !toStdout && !*force:
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s exists: pass --force to replace it, or another --out", path)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), cf.timeout)
	defer cancel()
	c, err := dialHelper(ctx, cf.socket)
	if err != nil {
		return err
	}
	defer c.Close()
	x, err := setup.ExportConfig(ctx, c, rpc.ConfigExportParams{NoSubscriptions: *noSubs})
	if err != nil {
		return err
	}
	if toStdout {
		if _, err := stdout.Write(x.Zip); err != nil {
			return err
		}
		fmt.Fprintln(stderr, exportSecretsWarning)
		return nil
	}
	if err := setup.SaveExport(path, x.Zip); err != nil {
		return err
	}
	res := ExportResult{Path: path, Bytes: len(x.Zip), Layers: x.Layers, Subscriptions: x.Subscriptions, NoContent: x.NoContent,
		Warnings: []string{rpc.ImportWarnSecrets}}
	if cf.json {
		b, _ := json.MarshalIndent(res, "", "  ")
		fmt.Fprintln(stdout, string(b))
		return nil
	}
	fmt.Fprintf(stdout, "exported %d override(s) and %d subscription(s) to %s\n", res.Layers, res.Subscriptions, path)
	if x.NoContent {
		fmt.Fprintln(stdout, "the subscriptions' downloads did not fit: the other machine downloads them again from their URLs")
	}
	fmt.Fprintln(stderr, exportSecretsWarning)
	fmt.Fprintf(stdout, "next, on the other machine: twinnel import %s\n", path)
	return nil
}
