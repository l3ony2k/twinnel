package linux

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestTrayHostPresent(t *testing.T) {
	answers := map[string]string{"busctl": "", "gdbus": "(true,)\n", "dbus-send": ""}
	var asked []string
	run := func(_ context.Context, argv []string) ([]byte, error) {
		asked = append(asked, argv[0])
		if argv[0] == "busctl" {
			return nil, errors.New("not found")
		}
		return []byte(answers[argv[0]]), nil
	}
	present, known := TrayHostPresent(context.Background(), run)
	if !present || !known || !reflect.DeepEqual(asked, []string{"busctl", "gdbus"}) {
		t.Fatalf("present %v known %v asked %v", present, known, asked)
	}
	none := func(context.Context, []string) ([]byte, error) { return nil, errors.New("no") }
	if p, k := TrayHostPresent(context.Background(), none); p || k {
		t.Errorf("no tools: present %v known %v", p, k)
	}
}

func TestParseNameHasOwner(t *testing.T) {
	for out, want := range map[string][2]bool{
		"b true\n":  {true, true},
		"b false\n": {false, true},
		"(true,)\n": {true, true},
		"(false,)":  {false, true},
		"method return time=1 sender=org.freedesktop.DBus -> destination=:1.9 serial=3 reply_serial=2\n   boolean true\n": {true, true},
		"Error org.freedesktop.DBus.Error.ServiceUnknown":                                                                 {false, false},
	} {
		v, ok := parseNameHasOwner(out)
		if v != want[0] || ok != want[1] {
			t.Errorf("parse(%q) = %v %v, want %v", out, v, ok, want)
		}
	}
}

func TestTrayHintFor(t *testing.T) {
	for _, c := range []struct {
		present, known bool
		desktop        string
		want           TrayHint
	}{
		{true, true, "GNOME", TrayHintNone},
		{false, false, "GNOME", TrayHintNone},
		{false, true, "GNOME", TrayHintGNOME},
		{false, true, "ubuntu:GNOME", TrayHintGNOME},
		{false, true, "Hyprland", TrayHintOther},
		{false, true, "KDE", TrayHintOther},
	} {
		if got := TrayHintFor(c.present, c.known, c.desktop); got != c.want {
			t.Errorf("%+v: got %v", c, got)
		}
	}
}

func TestTerminalArgv(t *testing.T) {
	have := map[string]bool{"/usr/bin/konsole": true, "/usr/bin/xterm": true, "/usr/bin/my-term": true}
	look := func(name string) (string, error) {
		p := name
		if name[0] != '/' {
			p = "/usr/bin/" + name
		}
		if have[p] {
			return p, nil
		}
		return "", errors.New("not found")
	}
	env := map[string]string{}
	getenv := func(k string) string { return env[k] }
	cmd := HelperJournalArgv()
	got, err := TerminalArgv(cmd, getenv, look)
	if err != nil {
		t.Fatal(err)
	}
	want := append([]string{"/usr/bin/konsole", "-e"}, cmd...)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q want %q", got, want)
	}
	env["TERMINAL"] = "my-term"
	if got, _ := TerminalArgv([]string{"x"}, getenv, look); !reflect.DeepEqual(got, []string{"/usr/bin/my-term", "-e", "x"}) {
		t.Errorf("$TERMINAL: %q", got)
	}
	env["TERMINAL"] = "sh -c evil" // not a program name: ignored
	if got, _ := TerminalArgv([]string{"x"}, getenv, look); got[0] != "/usr/bin/konsole" {
		t.Errorf("$TERMINAL with spaces: %q", got)
	}
	have = map[string]bool{}
	if _, err := TerminalArgv([]string{"x"}, getenv, look); !errors.Is(err, ErrNoTerminal) {
		t.Errorf("no terminal: %v", err)
	}
	if HelperJournalArgv()[2] != "twinnel-helper.service" {
		t.Errorf("journal unit: %q", HelperJournalArgv())
	}
}
