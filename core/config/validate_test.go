package config

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
)

// fakeMihomo writes a shell script that checks its arguments, records the
// files it sees in the work dir and prints the given output.
func fakeMihomo(t *testing.T, output string, exit int) (bin, seen string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake")
	}
	d := t.TempDir()
	seen = filepath.Join(d, "seen")
	script := `#!/bin/sh
[ "$1" = "-t" ] && [ "$2" = "-d" ] && [ "$4" = "-f" ] && [ "$5" = "$3/config.yaml" ] || { echo "bad args: $*"; exit 9; }
ls "$3" > ` + seen + `
echo "SAFE_PATHS=$SAFE_PATHS"
cat <<'OUT'
` + output + `
OUT
exit ` + string(rune('0'+exit)) + "\n"
	bin = filepath.Join(d, "mihomo")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, seen
}

func TestValidateOK(t *testing.T) {
	bin, seen := fakeMihomo(t, `time="x" level=info msg="Start initial configuration in progress"
configuration file /tmp/x/config.yaml test is successful`, 0)
	geo := t.TempDir()
	for _, f := range []string{"geoip.metadb", "ASN.mmdb", "GeoSite.dat", "cache.db", "config.yaml"} {
		os.WriteFile(filepath.Join(geo, f), []byte("x"), 0o644)
	}
	t.Setenv("SAFE_PATHS", "/x")
	v, err := Validate(context.Background(), bin, []byte("mode: rule\n"), WithGeoDir(geo), WithSafePaths("/a", "/b"))
	if err != nil {
		t.Fatal(err)
	}
	if !v.OK || len(v.Errors) != 0 {
		t.Errorf("v = %+v", v)
	}
	if want := "SAFE_PATHS=/x" + string(os.PathListSeparator) + "/a" + string(os.PathListSeparator) + "/b"; !strings.Contains(v.Output, want) {
		t.Errorf("output %q lacks %q", v.Output, want)
	}
	b, _ := os.ReadFile(seen)
	got := strings.Fields(string(b))
	sort.Strings(got)
	if want := []string{"ASN.mmdb", "GeoSite.dat", "config.yaml", "geoip.metadb"}; !reflect.DeepEqual(got, want) {
		t.Errorf("work dir had %v, want %v", got, want)
	}
	// The geo dir must be untouched.
	if b, _ := os.ReadFile(filepath.Join(geo, "config.yaml")); string(b) != "x" {
		t.Error("geo dir modified")
	}
}

func TestValidateErrors(t *testing.T) {
	bin, _ := fakeMihomo(t, `time="x" level=info msg="Start initial configuration in progress"
time="x" level=error msg="proxy group[0]: G: 'nonexist' not found"
time="x" level=fatal msg="Parse config error: rules[3] [\"bad\"] error"
configuration file /tmp/x/config.yaml test failed`, 1)
	v, err := Validate(context.Background(), bin, []byte("mode: rule\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{`proxy group[0]: G: 'nonexist' not found`, `Parse config error: rules[3] ["bad"] error`}
	if v.OK || !reflect.DeepEqual(v.Errors, want) {
		t.Errorf("v = %+v", v)
	}
}

func TestValidateFailWithoutErrorLines(t *testing.T) {
	bin, _ := fakeMihomo(t, "something odd happened", 1)
	v, err := Validate(context.Background(), bin, []byte("a: 1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if v.OK || !reflect.DeepEqual(v.Errors, []string{"something odd happened"}) {
		t.Errorf("v = %+v", v)
	}
}

func TestValidateRunErrors(t *testing.T) {
	if _, err := Validate(context.Background(), filepath.Join(t.TempDir(), "missing"), nil); err == nil {
		t.Error("missing binary: no error")
	}
	if runtime.GOOS == "windows" {
		return
	}
	bin := filepath.Join(t.TempDir(), "slow")
	os.WriteFile(bin, []byte("#!/bin/sh\nexec sleep 5\n"), 0o755)
	if _, err := Validate(context.Background(), bin, nil, WithTimeout(100*time.Millisecond)); err == nil {
		t.Error("timeout: no error")
	}
}

// TestValidateReal runs the real binary when DUET_MIHOMO points to one.
func TestValidateReal(t *testing.T) {
	bin := os.Getenv("DUET_MIHOMO")
	if bin == "" {
		t.Skip("DUET_MIHOMO not set")
	}
	geo := os.Getenv("DUET_GEO_DIR")
	cfg, err := os.ReadFile(filepath.Join("testdata", "composed.golden.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	v, err := Validate(context.Background(), bin, cfg, WithGeoDir(geo))
	if err != nil {
		t.Fatal(err)
	}
	if !v.OK {
		t.Errorf("golden config invalid: %v\n%s", v.Errors, v.Output)
	}
	v, err = Validate(context.Background(), bin, []byte("proxy-groups:\n  - {name: G, type: select, proxies: [nope]}\nrules: ['MATCH,G']\n"), WithGeoDir(geo))
	if err != nil {
		t.Fatal(err)
	}
	if v.OK || len(v.Errors) == 0 {
		t.Errorf("bad config passed: %+v", v)
	}
}

// WithRunAs: the work dir is in /tmp, handed to the user, and the command
// is prepared last (after SAFE_PATHS is set).
func TestValidateRunAs(t *testing.T) {
	bin, _ := fakeMihomo(t, `configuration file x test is successful`, 0)
	var dir string
	prepared := false
	v, err := Validate(context.Background(), bin, []byte("mode: rule\n"), WithSafePaths("/a"),
		WithRunAs(os.Getuid(), os.Getgid(), func(cmd *exec.Cmd) {
			prepared = true
			dir = cmd.Dir
			if !slices.ContainsFunc(cmd.Env, func(kv string) bool { return strings.HasPrefix(kv, "SAFE_PATHS=") }) {
				t.Error("SAFE_PATHS not set before prepare")
			}
		}))
	if err != nil || !v.OK || !prepared {
		t.Fatalf("v=%+v err=%v prepared=%v", v, err, prepared)
	}
	if !strings.HasPrefix(dir, "/tmp/duet-validate-") {
		t.Errorf("work dir %s not in /tmp", dir)
	}
}
