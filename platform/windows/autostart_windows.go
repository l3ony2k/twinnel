//go:build windows

package windows

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"

	"github.com/l3ony2k/twinnel/core/autostart"
)

// AutostartSource reads Windows' autostarts: the Task Scheduler's task
// files and the Run keys of the machine and of every loaded user hive
// (HKEY_USERS\<SID>: the helper runs as LocalSystem, whose own HKCU is
// not the user's). Read only.
func AutostartSource() autostart.Source {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	return autostart.Multi{&autostart.TaskDirSource{Dir: filepath.Join(root, "System32", "Tasks")}, runKeys{}}
}

type runKeys struct{}

const (
	runPath   = `Software\Microsoft\Windows\CurrentVersion\Run`
	run32Path = `Software\WOW6432Node\Microsoft\Windows\CurrentVersion\Run`
	approved  = `Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\`
)

func (runKeys) List(ctx context.Context) ([]autostart.Entry, error) {
	var out []autostart.Entry
	var errs []error
	add := func(root registry.Key, hive, sid, path, approvedName string) {
		vals, err := readValues(root, path)
		if err != nil {
			if !errors.Is(err, registry.ErrNotExist) {
				errs = append(errs, err)
			}
			return
		}
		state, _ := readBinary(root, approved+approvedName)
		for name, cmd := range vals {
			e := autostart.Entry{Kind: autostart.RunKey, Name: name, Program: exeOf(cmd), Location: hive + `\` + path,
				User: sid, Enabled: true, Disable: autostart.ByHelper}
			if b, ok := state[name]; ok && len(b) > 0 && b[0]&1 == 1 {
				e.Enabled = false // StartupApproved 03…: turned off
			}
			out = append(out, e)
		}
	}
	add(registry.LOCAL_MACHINE, "HKLM", "", runPath, "Run")
	add(registry.LOCAL_MACHINE, "HKLM", "", run32Path, "Run32")
	users, err := registry.USERS.ReadSubKeyNames(-1)
	if err != nil {
		errs = append(errs, err)
	}
	for _, sid := range users {
		// Real accounts only (S-1-5-21-…), not their _Classes hives.
		if !strings.HasPrefix(sid, "S-1-5-21-") || strings.HasSuffix(sid, "_Classes") {
			continue
		}
		k, err := registry.OpenKey(registry.USERS, sid, registry.READ)
		if err != nil {
			continue
		}
		add(k, `HKU\`+sid, sid, runPath, "Run")
		k.Close()
	}
	return out, errors.Join(errs...)
}

func readValues(root registry.Key, path string) (map[string]string, error) {
	k, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
	if err != nil {
		return nil, err
	}
	defer k.Close()
	names, err := k.ReadValueNames(-1)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, n := range names {
		if v, _, err := k.GetStringValue(n); err == nil {
			out[n] = v
		}
	}
	return out, nil
}

func readBinary(root registry.Key, path string) (map[string][]byte, error) {
	k, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
	if err != nil {
		return nil, err
	}
	defer k.Close()
	names, err := k.ReadValueNames(-1)
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	for _, n := range names {
		if v, _, err := k.GetBinaryValue(n); err == nil {
			out[n] = v
		}
	}
	return out, nil
}
