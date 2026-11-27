package engine

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/l3ony2k/twinnel/core/config"
	"github.com/l3ony2k/twinnel/core/kernel"
	"github.com/l3ony2k/twinnel/core/privfile"
)

// TempProxyConfig turns a composed config into the config of a temporary
// download proxy: one mixed port on loopback, no other inbound, no TUN, no
// DNS listener, no controller, and every connection to the first proxy
// group (else the first proxy). Rules and rule providers are dropped, so
// it needs no geo database. The result holds the subscription's node
// credentials: write it only where root (or the user it runs as) can read.
func TempProxyConfig(composed []byte, port int) ([]byte, error) {
	var head struct {
		Groups []struct {
			Name string `yaml:"name"`
		} `yaml:"proxy-groups"`
		Proxies []struct {
			Name string `yaml:"name"`
		} `yaml:"proxies"`
	}
	if err := yaml.Unmarshal(composed, &head); err != nil {
		return nil, fmt.Errorf("composed config: %w", err)
	}
	target := ""
	switch {
	case len(head.Groups) > 0 && head.Groups[0].Name != "":
		target = head.Groups[0].Name
	case len(head.Proxies) > 0 && head.Proxies[0].Name != "":
		target = head.Proxies[0].Name
	default:
		return nil, errNoProxy
	}
	patch := map[string]any{
		"mixed-port!":          port,
		"port!":                0,
		"socks-port!":          0,
		"redir-port!":          0,
		"tproxy-port!":         0,
		"allow-lan!":           false,
		"bind-address!":        "127.0.0.1",
		"external-controller!": "",
		"external-ui!":         "",
		"mode!":                "rule",
		"tun!":                 map[string]any{"enable": false},
		"dns!":                 map[string]any{"enable": false},
		"listeners!":           []any{},
		"rule-providers!":      map[string]any{},
		"rules!":               []string{"MATCH," + target},
	}
	pb, err := yaml.Marshal(patch)
	if err != nil {
		return nil, err
	}
	return config.MergeBytes(composed, pb)
}

// startTempMihomo runs bin with cfg in a new private dir until stop. As
// root it runs as nobody (or as the binary's owner when root does not
// control the binary), never as root: it only needs a loopback port.
func startTempMihomo(ctx context.Context, bin string, cfg []byte, port int) (func(), error) {
	trust, err := kernel.CheckExec(bin)
	if err != nil {
		return nil, err
	}
	dir, err := privfile.MkdirTemp("", "twinnel-proxy-")
	if err != nil {
		return nil, err
	}
	cleanup := func() { os.RemoveAll(dir) }
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := privfile.WriteFile(cfgPath, cfg); err != nil {
		cleanup()
		return nil, err
	}
	cmd := exec.Command(trust.Path, "-d", dir, "-f", cfgPath)
	if geteuid() == 0 {
		cred := kernel.Nobody
		if !trust.Trusted {
			cred = trust.RunAs
		}
		for _, p := range []string{dir, cfgPath} {
			if err := os.Chown(p, cred.UID, cred.GID); err != nil {
				cleanup()
				return nil, err
			}
		}
		kernel.DropTo(cmd, cred)
	}
	if err := cmd.Start(); err != nil {
		cleanup()
		return nil, err
	}
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	stop := func() {
		cmd.Process.Kill()
		<-done
		cleanup()
	}
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	deadline := time.Now().Add(15 * time.Second)
	for {
		c, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
		if err == nil {
			c.Close()
			return stop, nil
		}
		select {
		case <-done:
			cleanup()
			return nil, errors.New("mihomo exited before it listened")
		case <-ctx.Done():
			stop()
			return nil, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			stop()
			return nil, fmt.Errorf("mihomo did not listen on %s within 15s", addr)
		}
	}
}
