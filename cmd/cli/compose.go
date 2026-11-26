package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/l3ony2k/duet/core/config"
)

func init() {
	register("compose", "compose a mihomo config from a subscription and override layers", runCompose)
}

const composeUsage = `usage: duet compose --sub FILE [flags]

Builds the final mihomo config: subscription → --base → --layer/--verge (in
the order given) → Tailscale coexist → leak guard. Nothing is written unless
every step (and --validate, if given) succeeds.

flags:
`

func runCompose(args []string) error {
	fs := flag.NewFlagSet("compose", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), composeUsage)
		fs.PrintDefaults()
	}
	var (
		sub, base, out, validate, geoDir string
		tailnetJSON, tsResolver          string
		leakGuard                        string
		fromStatus, force                bool
		userLayers                       []userLayer
		safePaths                        []string
	)
	fs.StringVar(&sub, "sub", "", "subscription `FILE` (required)")
	fs.StringVar(&base, "base", "", "base settings layer `FILE` (ports, DNS, TUN, controller, UI)")
	fs.Func("layer", "user override `FILE` in Merge syntax (repeatable, applied in order)", func(s string) error {
		userLayers = append(userLayers, userLayer{path: s})
		return nil
	})
	fs.Func("verge", "user override `FILE` in Clash Verge Rev Merge syntax (repeatable)", func(s string) error {
		userLayers = append(userLayers, userLayer{path: s, verge: true})
		return nil
	})
	fs.BoolVar(&fromStatus, "tailnet-from-status", false, "add the Tailscale coexist layer from `tailscale status --json` (read-only)")
	fs.StringVar(&tailnetJSON, "tailnet-json", "", "add the Tailscale coexist layer from a saved `tailscale status --json` FILE")
	fs.StringVar(&tsResolver, "tailnet-resolver", "", "nameserver for tailnet names (default \"system\")")
	fs.StringVar(&leakGuard, "leak-guard", "", "comma-separated proxy `GROUPS` that must never use DIRECT")
	fs.StringVar(&validate, "validate", "", "check the result with this mihomo `BIN` (mihomo -t in a temp dir)")
	fs.StringVar(&geoDir, "geo-dir", "", "`DIR` with geo databases to copy into the validation dir")
	fs.Func("safe-path", "`DIR` mihomo may reference during --validate, e.g. the external-ui dir (repeatable)", func(s string) error {
		safePaths = append(safePaths, s)
		return nil
	})
	fs.StringVar(&out, "o", "", "output `FILE` (default stdout)")
	fs.BoolVar(&force, "force", false, "overwrite an existing -o file")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("compose: unexpected arguments %q", fs.Args())
	}
	if sub == "" {
		fs.Usage()
		return errors.New("compose: --sub is required")
	}
	if fromStatus && tailnetJSON != "" {
		return errors.New("compose: use either --tailnet-from-status or --tailnet-json")
	}
	if out != "" && !force {
		if _, err := os.Stat(out); err == nil {
			return fmt.Errorf("compose: %s exists (use --force to overwrite)", out)
		}
	}

	subData, err := os.ReadFile(sub)
	if err != nil {
		return err
	}
	var layers []config.Layer
	if base != "" {
		b, err := os.ReadFile(base)
		if err != nil {
			return err
		}
		layers = append(layers, config.OverrideLayer("base", "Base settings", config.KindManaged, config.StageBase, b))
	}
	for i, ul := range userLayers {
		b, err := os.ReadFile(ul.path)
		if err != nil {
			return err
		}
		if ul.verge {
			if b, err = config.ImportVergeMerge(b); err != nil {
				return fmt.Errorf("%s: %w", ul.path, err)
			}
		}
		id := fmt.Sprintf("user-%d-%s", i+1, strings.TrimSuffix(filepath.Base(ul.path), filepath.Ext(ul.path)))
		layers = append(layers, config.OverrideLayer(id, ul.path, config.KindUser, config.StageUser, b))
	}
	if fromStatus || tailnetJSON != "" {
		var status []byte
		if fromStatus {
			status, err = tailscaleStatus()
		} else {
			status, err = os.ReadFile(tailnetJSON)
		}
		if err != nil {
			return err
		}
		info, err := config.TailnetInfoFromStatus(status)
		if err != nil {
			return err
		}
		info.Resolver = tsResolver
		l, err := config.TailscaleCoexistLayer(info)
		if err != nil {
			return err
		}
		layers = append(layers, l)
	}
	if leakGuard != "" {
		var groups []string
		for _, g := range strings.Split(leakGuard, ",") {
			if g = strings.TrimSpace(g); g != "" {
				groups = append(groups, g)
			}
		}
		layers = append(layers, config.LeakGuardLayer(groups))
	}

	res, err := config.ComposeReport(subData, layers)
	if err != nil {
		return err
	}
	for _, w := range res.Warnings {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}
	fmt.Fprintln(os.Stderr, "layers applied:", strings.Join(res.Applied, ", "))

	if validate != "" {
		var opts []config.ValidateOption
		if geoDir != "" {
			opts = append(opts, config.WithGeoDir(geoDir))
		}
		if len(safePaths) > 0 {
			opts = append(opts, config.WithSafePaths(safePaths...))
		}
		v, err := config.Validate(context.Background(), validate, res.Config, opts...)
		if err != nil {
			return err
		}
		if !v.OK {
			for _, e := range v.Errors {
				fmt.Fprintln(os.Stderr, "mihomo:", e)
			}
			return errors.New("compose: mihomo -t rejected the config; nothing written")
		}
		fmt.Fprintln(os.Stderr, "mihomo -t: ok")
	}

	if out == "" {
		_, err = os.Stdout.Write(res.Config)
		return err
	}
	return writeFileAtomic(out, res.Config)
}

type userLayer struct {
	path  string
	verge bool
}

// tailscaleStatus runs `tailscale status --json` (read-only).
func tailscaleStatus() ([]byte, error) {
	bin, err := exec.LookPath("tailscale")
	if err != nil {
		for _, p := range []string{
			"/opt/homebrew/bin/tailscale",
			"/usr/local/bin/tailscale",
			"/Applications/Tailscale.app/Contents/MacOS/Tailscale",
		} {
			if _, e := os.Stat(p); e == nil {
				bin, err = p, nil
				break
			}
		}
	}
	if err != nil {
		return nil, errors.New("tailscale CLI not found")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, bin, "status", "--json").Output()
	if err != nil {
		return nil, fmt.Errorf("tailscale status --json: %w", err)
	}
	return b, nil
}

func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
