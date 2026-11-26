package config

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ValidateOption configures Validate.
type ValidateOption func(*validateOpts)

type validateOpts struct {
	geoDir  string
	timeout time.Duration
}

// WithGeoDir copies the geo databases (*.mmdb, *.metadb, *.dat) found in dir
// into the temporary work dir, so mihomo does not try to download them.
// They are copied, not symlinked, so nothing mihomo does can write back into
// dir.
func WithGeoDir(dir string) ValidateOption { return func(o *validateOpts) { o.geoDir = dir } }

// WithTimeout bounds the mihomo run (default 60s).
func WithTimeout(d time.Duration) ValidateOption { return func(o *validateOpts) { o.timeout = d } }

// Validation is the outcome of a mihomo config test.
type Validation struct {
	OK     bool
	Errors []string // messages of level=error/fatal lines
	Output string   // full combined output
}

// Validate checks config with the official mihomo binary:
// `mihomo -t -d <tmp> -f <tmp>/config.yaml`. Everything happens in a fresh
// temporary directory that is removed afterwards. The returned error is only
// for failures to run the test; an invalid config gives OK=false.
func Validate(ctx context.Context, mihomoBin string, config []byte, opts ...ValidateOption) (*Validation, error) {
	o := validateOpts{timeout: 60 * time.Second}
	for _, f := range opts {
		f(&o)
	}
	dir, err := os.MkdirTemp("", "duet-validate-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	if o.geoDir != "" {
		if err := copyGeo(o.geoDir, dir); err != nil {
			return nil, fmt.Errorf("geo files: %w", err)
		}
	}
	cfg := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfg, config, 0o600); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, o.timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, mihomoBin, "-t", "-d", dir, "-f", cfg)
	cmd.Dir = dir
	cmd.WaitDelay = 2 * time.Second
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	runErr := cmd.Run()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("mihomo -t: %w", ctx.Err())
	}
	var exitErr *exec.ExitError
	if runErr != nil && !errors.As(runErr, &exitErr) {
		return nil, fmt.Errorf("mihomo -t: %w", runErr)
	}
	v := parseTestOutput(out.String())
	v.OK = runErr == nil && len(v.Errors) == 0 && strings.Contains(v.Output, "test is successful")
	if !v.OK && len(v.Errors) == 0 {
		v.Errors = []string{lastLine(v.Output)}
	}
	return v, nil
}

var logLineRE = regexp.MustCompile(`level=(error|fatal|panic) msg="((?:[^"\\]|\\.)*)"`)

func parseTestOutput(s string) *Validation {
	v := &Validation{Output: s}
	for _, m := range logLineRE.FindAllStringSubmatch(s, -1) {
		msg := strings.ReplaceAll(m[2], `\"`, `"`)
		v.Errors = append(v.Errors, msg)
	}
	return v
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		return s[i+1:]
	}
	if s == "" {
		return "mihomo -t failed without output"
	}
	return s
}

func copyGeo(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext != ".mmdb" && ext != ".metadb" && ext != ".dat" {
			continue
		}
		info, err := os.Stat(filepath.Join(src, e.Name()))
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if err := copyFile(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
