package service

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestStepStringAndFormatPlan(t *testing.T) {
	steps := []Step{
		{Kind: StepMkdir, Path: "/Library/Application Support/twinnel", Mode: 0o755},
		{Kind: StepCopy, Src: "/tmp/twinnel-helper", Path: "/Library/PrivilegedHelperTools/x", Mode: 0o755},
		{Kind: StepWrite, Path: "/Library/LaunchDaemons/x.plist", Mode: 0o644, Content: []byte("<plist/>")},
		{Kind: StepRun, Argv: []string{"/bin/launchctl", "bootout", "system/x"}, IgnoreError: true},
		{Kind: StepRun, Argv: []string{"echo", "it's"}},
		{Kind: StepWait, Argv: []string{"/bin/launchctl", "print", "system/x"}, Until: "Could not find service", Timeout: 10 * time.Second, IgnoreError: true},
		{Kind: StepRun, Argv: []string{"/bin/launchctl", "bootstrap", "system", "/x.plist"}, Retry: 4, RetryOn: []string{": 5: "}},
	}
	var b bytes.Buffer
	FormatPlan(&b, steps)
	want := `1. install -d -m 0755 '/Library/Application Support/twinnel'
2. install -m 0755 /tmp/twinnel-helper /Library/PrivilegedHelperTools/x
3. write /Library/LaunchDaemons/x.plist (mode 0644, 8 bytes)
----- /Library/LaunchDaemons/x.plist -----
<plist/>
----- end -----
4. /bin/launchctl bootout system/x  # errors ignored
5. echo 'it'\''s'
6. wait until ` + "`/bin/launchctl print system/x`" + ` says "Could not find service" (up to 10s)  # errors ignored
7. /bin/launchctl bootstrap system /x.plist  # retried up to 4 times while busy
`
	if b.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", b.String(), want)
	}
	s := MihomoSpec("l", "/b", "/c", "/c/config.yaml", "")
	if s.StdoutPath != "" || strings.Join(s.Args, " ") != "-d /c -f /c/config.yaml" || !s.KeepAlive {
		t.Errorf("%+v", s)
	}
}

// fakeEx answers each call from a list (the last one repeats).
type fakeEx struct {
	outs  []string
	errs  []error
	calls int
}

func (f *fakeEx) Run(context.Context, []string) ([]byte, error) {
	i := min(f.calls, len(f.outs)-1)
	f.calls++
	return []byte(f.outs[i]), f.errs[i]
}

func TestRetryAndWait(t *testing.T) {
	old := Sleep
	var slept []time.Duration
	Sleep = func(_ context.Context, d time.Duration) error { slept = append(slept, d); return nil }
	defer func() { Sleep = old }()
	e5 := errors.New("exit status 5")
	retry := Step{Kind: StepRun, Argv: []string{"x"}, Retry: 3, RetryOn: []string{"exit status 5"}, RetryBackoff: time.Second}

	ex := &fakeEx{outs: []string{"busy", "busy", ""}, errs: []error{e5, e5, nil}}
	if err := Apply(context.Background(), []Step{retry}, ex, nil); err != nil || ex.calls != 3 {
		t.Errorf("retry: %v after %d", err, ex.calls)
	}
	if len(slept) != 2 || slept[0] != time.Second || slept[1] != 2*time.Second {
		t.Errorf("backoff %v", slept)
	}
	ex = &fakeEx{outs: []string{"busy"}, errs: []error{e5}}
	if err := Apply(context.Background(), []Step{retry}, ex, nil); err == nil || ex.calls != 4 || !strings.Contains(err.Error(), "after 4 attempts") {
		t.Errorf("gives up: %v after %d", err, ex.calls)
	}

	wait := Step{Kind: StepWait, Argv: []string{"x"}, Until: "gone", Timeout: time.Second}
	ex = &fakeEx{outs: []string{"here", "here", "it is gone"}, errs: []error{nil, nil, e5}}
	if err := Apply(context.Background(), []Step{wait}, ex, nil); err != nil || ex.calls != 3 {
		t.Errorf("wait: %v after %d", err, ex.calls)
	}
	ex = &fakeEx{outs: []string{"here"}, errs: []error{nil}}
	if err := Apply(context.Background(), []Step{wait}, ex, nil); err == nil || !strings.Contains(err.Error(), "still not") {
		t.Errorf("wait timeout: %v", err)
	}
	wait.IgnoreError = true
	if err := Apply(context.Background(), []Step{wait}, ex, nil); err != nil {
		t.Errorf("ignored wait timeout: %v", err)
	}
}
