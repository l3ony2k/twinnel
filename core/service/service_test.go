package service

import (
	"bytes"
	"strings"
	"testing"
)

func TestStepStringAndFormatPlan(t *testing.T) {
	steps := []Step{
		{Kind: StepMkdir, Path: "/Library/Application Support/duet", Mode: 0o755},
		{Kind: StepCopy, Src: "/tmp/duet-helper", Path: "/Library/PrivilegedHelperTools/x", Mode: 0o755},
		{Kind: StepWrite, Path: "/Library/LaunchDaemons/x.plist", Mode: 0o644, Content: []byte("<plist/>")},
		{Kind: StepRun, Argv: []string{"/bin/launchctl", "bootout", "system/x"}, IgnoreError: true},
		{Kind: StepRun, Argv: []string{"echo", "it's"}},
	}
	var b bytes.Buffer
	FormatPlan(&b, steps)
	want := `1. install -d -m 0755 '/Library/Application Support/duet'
2. install -m 0755 /tmp/duet-helper /Library/PrivilegedHelperTools/x
3. write /Library/LaunchDaemons/x.plist (mode 0644, 8 bytes)
----- /Library/LaunchDaemons/x.plist -----
<plist/>
----- end -----
4. /bin/launchctl bootout system/x  # errors ignored
5. echo 'it'\''s'
`
	if b.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", b.String(), want)
	}
	s := MihomoSpec("l", "/b", "/c", "/c/config.yaml", "")
	if s.StdoutPath != "" || strings.Join(s.Args, " ") != "-d /c -f /c/config.yaml" || !s.KeepAlive {
		t.Errorf("%+v", s)
	}
}
