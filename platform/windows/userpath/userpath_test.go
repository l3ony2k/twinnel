package userpath

import (
	"strings"
	"testing"
)

func TestAddRemove(t *testing.T) {
	expand := func(s string) string { return strings.ReplaceAll(s, "%LOCALAPPDATA%", `C:\Users\u\AppData\Local`) }
	const bin = `C:\Users\u\AppData\Local\Programs\Twinnel\bin`
	v := `%LOCALAPPDATA%\Microsoft\WindowsApps;;C:\Tools\`

	got, changed := Add(v, bin, expand)
	if !changed || got != `%LOCALAPPDATA%\Microsoft\WindowsApps;C:\Tools\;`+bin {
		t.Fatalf("Add = %q %v", got, changed)
	}
	if again, changed := Add(got, strings.ToUpper(bin)+`\`, expand); changed || again != got {
		t.Fatalf("second Add = %q %v", again, changed)
	}
	// The same folder written with a variable counts as there.
	if !Contains(`%LOCALAPPDATA%\Programs\Twinnel\bin`, bin, expand) {
		t.Fatal("Contains misses the unexpanded entry")
	}
	back, changed := Remove(got, bin+`\`, expand)
	if !changed || back != `%LOCALAPPDATA%\Microsoft\WindowsApps;C:\Tools\` {
		t.Fatalf("Remove = %q %v", back, changed)
	}
	if same, changed := Remove(back, bin, expand); changed || same != back {
		t.Fatalf("Remove of a missing entry = %q %v", same, changed)
	}
	if got, _ := Add("", bin, nil); got != bin {
		t.Fatalf("Add to empty = %q", got)
	}
}
