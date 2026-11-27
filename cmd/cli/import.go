package main

// twinnel import: bring subscriptions and overrides from another machine
// (a Twinnel export zip), a subscription URL list, or one Clash Verge Rev
// / Clash Party override file. The helper parses and stores (config.import).

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/l3ony2k/twinnel/core/bundle"
	"github.com/l3ony2k/twinnel/core/rpc"
)

func init() {
	register("import", "import an export zip, subscription URLs or an override file (--dry-run to preview)", runImport)
}

const importUsage = `usage: twinnel import FILE [--dry-run] [--on-conflict ask|replace|skip|rename] [--yes] [--json]

Imports subscriptions and your own overrides (names, order, on/off) from:
  - a Twinnel export (.zip, from 停止使用 → 导出配置 on the old machine)
  - a list of subscription URLs, one per line ("name https://…" names one)
  - one Clash Verge Rev "Merge" or Clash Party override file (.yaml)
  - a whole mihomo/Clash config (.yaml): becomes a subscription
Twinnel's own layers (Tailscale coexist, leak guard, takeover base) are
rebuilt on this machine, not imported. Nothing is applied: run
'twinnel compose --apply' afterwards.

The file may hold subscription URLs and node passwords: keep it private.
Ids already in use are conflicts: --on-conflict decides (ask: one by one
on a terminal).

`

func runImport(args []string) error {
	fs, cf := newFlagSet("import", importUsage)
	dry := fs.Bool("dry-run", false, "show what would be imported, change nothing")
	onConflict := fs.String("on-conflict", rpc.ImportAsk, "for ids already in use: ask, replace, skip or rename")
	yes := fs.Bool("yes", false, "do not ask before importing")
	pos, done, err := parse(fs, args)
	if done || err != nil {
		return err
	}
	if len(pos) != 1 {
		fs.Usage()
		return errors.New("want one FILE")
	}
	if err := checkResolutionFlag(*onConflict); err != nil {
		return err
	}
	if cf.json && !*dry && !*yes {
		return errors.New("--json without --dry-run needs --yes (nothing can be asked)")
	}
	data, err := readImportFile(pos[0])
	if err != nil {
		return err
	}
	p := rpc.ConfigImportParams{Data: data, Name: filepath.Base(pos[0]), DryRun: true, OnConflict: *onConflict}
	if *dry {
		var r rpc.ConfigImportResult
		return cf.callPrint(rpc.MethodConfigImport, p, &r, func() { printImport(stdout, r) })
	}

	in := bufio.NewReader(stdin) // one reader: the answers come in one stream
	var plan rpc.ConfigImportResult
	if err := cf.call(rpc.MethodConfigImport, p, &plan); err != nil {
		return err
	}
	if !cf.json {
		printImport(stdout, plan)
	}
	if plan.Conflicts > 0 {
		if cf.json || !isTerminal(stdin) {
			return fmt.Errorf("%d item(s) conflict with ids in use: pass --on-conflict replace, skip or rename", plan.Conflicts)
		}
		p.Resolve = askConflicts(in, plan)
	}
	if countStored(plan, p) == 0 && plan.Activated == "" {
		if cf.json {
			b, _ := json.MarshalIndent(plan, "", "  ")
			fmt.Fprintln(stdout, string(b))
			return nil
		}
		fmt.Fprintln(stdout, "nothing to import")
		return nil
	}
	if !*yes && !confirmFrom(in) {
		return errors.New("not imported")
	}
	p.DryRun = false
	if cf.json {
		var raw json.RawMessage
		if err := cf.call(rpc.MethodConfigImport, p, &raw); err != nil {
			return err
		}
		return printJSON(raw)
	}
	var r rpc.ConfigImportResult
	if err := cf.call(rpc.MethodConfigImport, p, &r); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "imported %d item(s)", r.Stored)
	if r.Activated != "" {
		fmt.Fprintf(stdout, "; active subscription: %s", r.Activated)
	}
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "next: twinnel compose --preview, then twinnel compose --apply")
	return nil
}

func checkResolutionFlag(v string) error {
	switch v {
	case rpc.ImportAsk, rpc.ImportReplace, rpc.ImportSkip, rpc.ImportRename:
		return nil
	}
	return fmt.Errorf("--on-conflict: want ask, replace, skip or rename, not %q", v)
}

// readImportFile reads at most bundle.MaxFile bytes.
func readImportFile(name string) ([]byte, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, bundle.MaxFile+1))
	if err != nil {
		return nil, err
	}
	if len(b) > bundle.MaxFile {
		return nil, fmt.Errorf("%s is larger than %d MiB", name, bundle.MaxFile>>20)
	}
	return b, nil
}

// countStored counts the items the import would write once p's
// resolutions are applied.
func countStored(plan rpc.ConfigImportResult, p rpc.ConfigImportParams) int {
	n := 0
	for _, it := range plan.Items {
		a := it.Action
		if a == rpc.ImportConflict {
			a = p.Resolve[it.Kind+":"+it.ID]
		}
		if a == rpc.ImportAdd || a == rpc.ImportReplace || a == rpc.ImportRename {
			n++
		}
	}
	return n
}

func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return true // a test's reader
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// askConflicts asks replace / skip / rename for each conflict.
func askConflicts(in *bufio.Reader, plan rpc.ConfigImportResult) map[string]string {
	out := map[string]string{}
	for _, it := range plan.Items {
		if it.Action != rpc.ImportConflict {
			continue
		}
		for {
			fmt.Fprintf(stdout, "%s %q is already here (%s): [r]eplace, [s]kip or re[n]ame? ", it.Kind, it.ID, it.Existing)
			line, err := in.ReadString('\n')
			a := map[string]string{"r": rpc.ImportReplace, "replace": rpc.ImportReplace, "s": rpc.ImportSkip, "skip": rpc.ImportSkip,
				"n": rpc.ImportRename, "rename": rpc.ImportRename}[strings.ToLower(strings.TrimSpace(line))]
			if a != "" {
				out[it.Kind+":"+it.ID] = a
				break
			}
			if err != nil { // end of input: keep what is here
				out[it.Kind+":"+it.ID] = rpc.ImportSkip
				fmt.Fprintln(stdout)
				break
			}
		}
	}
	return out
}

// confirmFrom is confirm() on a shared reader.
func confirmFrom(in *bufio.Reader) bool {
	if !isTerminal(stdin) {
		fmt.Fprintln(stdout, "# not a terminal: pass --yes to proceed")
		return false
	}
	fmt.Fprint(stdout, "Import? [y/N] ")
	line, _ := in.ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes"
}

// printImport prints the plan. URLs are never in it.
func printImport(w io.Writer, r rpc.ConfigImportResult) {
	src := r.Source
	if r.Version > 0 {
		src += fmt.Sprintf(" v%d", r.Version)
	}
	if r.App != "" {
		src += ", Twinnel " + r.App
	}
	fmt.Fprintf(w, "source: %s\n", src)
	t := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(t, "KIND\tID\tNAME\tACTION\tDETAIL")
	for _, it := range r.Items {
		action := it.Action
		if it.Action == rpc.ImportRename {
			action += " → " + it.NewID
		}
		var detail []string
		switch it.Kind {
		case rpc.ImportKindLayer:
			if it.Format != "" {
				detail = append(detail, it.Format)
			}
			if !it.Enabled && it.Action != rpc.ImportGenerated && it.Action != rpc.ImportInvalid {
				detail = append(detail, "off")
			}
		case rpc.ImportKindSubscription:
			if it.HasURL {
				detail = append(detail, "URL")
			}
			if it.HasContent {
				detail = append(detail, "downloaded")
			}
			if it.Active {
				detail = append(detail, "active")
			}
		}
		if it.Action == rpc.ImportConflict && it.Existing != "" {
			detail = append(detail, "here: "+it.Existing)
		}
		if it.Reason != "" {
			detail = append(detail, it.Reason)
		}
		fmt.Fprintf(t, "%s\t%s\t%s\t%s\t%s\n", it.Kind, it.ID, it.Name, action, orDash(strings.Join(detail, ", ")))
	}
	t.Flush()
	if r.Activated != "" {
		fmt.Fprintf(w, "becomes active: %s\n", r.Activated)
	}
	for _, code := range r.Warnings {
		fmt.Fprintln(w, "warning: "+importWarning(code))
	}
	if r.Conflicts > 0 {
		fmt.Fprintf(w, "%d conflict(s)\n", r.Conflicts)
	}
}

func importWarning(code string) string {
	switch code {
	case rpc.ImportWarnSecrets:
		return "the file holds subscription URLs or node passwords; keep it private and delete it once imported"
	case rpc.ImportWarnPlainHTTP:
		return "a subscription URL uses plain http; use https if the provider offers it"
	case rpc.ImportWarnNoContent:
		return "a subscription has a URL only: refresh it (twinnel sub refresh ID) before applying"
	case rpc.ImportWarnNoSubscriptions:
		return "no subscription yet: add one (twinnel sub put) before applying"
	}
	return code
}
