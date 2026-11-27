package linux

import (
	"reflect"
	"testing"
)

func TestPkexecArgv(t *testing.T) {
	// Spaces, quotes and $ go through as they are: no shell parses argv.
	args := []string{"helper", "install", "--as-root", "--helper-bin", "/opt/my tools/it's $HOME/twinnel-helper"}
	got, err := PkexecArgv("/opt/my tools/twinnel", args)
	if err != nil {
		t.Fatal(err)
	}
	want := append([]string{"/usr/bin/pkexec", "/opt/my tools/twinnel"}, args...)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv = %q, want %q", got, want)
	}
	if _, err := PkexecArgv("twinnel", nil); err == nil {
		t.Error("a relative program path was accepted")
	}
	if _, err := PkexecArgv("/usr/bin/twinnel", []string{"a\nb"}); err == nil {
		t.Error("a newline in an argument was accepted")
	}
}

func TestPkexecFailure(t *testing.T) {
	for _, c := range []struct {
		code             int
		stderr, msg      string
		cancelled, known bool
	}{
		{126, "", "", true, true},
		{127, "Error executing command as another user: No authentication agent found.\n", ErrNoAuthAgent.Error(), false, true},
		{127, "Error executing command as another user: Not authorized\n", "not authorized: polkit refused the request", false, true},
		{127, "", "", false, true},
		{1, "# twinnel helper install: running as root\ntwinnel: helper install failed: systemctl: exit 1\n", "helper install failed: systemctl: exit 1", false, false},
		{2, "boom\n", "", false, false},
	} {
		msg, cancelled, known := PkexecFailure(c.code, c.stderr)
		if msg != c.msg || cancelled != c.cancelled || known != c.known {
			t.Errorf("PkexecFailure(%d, %q) = %q, %v, %v; want %q, %v, %v", c.code, c.stderr, msg, cancelled, known, c.msg, c.cancelled, c.known)
		}
	}
}
