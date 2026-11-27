package mihomo

import (
	"reflect"
	"testing"
)

func TestLeaves(t *testing.T) {
	all := map[string]Proxy{
		"PROXY":   {Name: "PROXY", Type: "Selector", Now: "us-1", All: []string{"us-1", "us-2", "sg-1", "REJECT"}},
		"YouTube": {Name: "YouTube", Type: "Selector", Now: "sg-1", All: []string{"PROXY", "us-1", "sg-1", "DIRECT"}},
		"Loop":    {Name: "Loop", Type: "Selector", Now: "Back", All: []string{"Back", "jp-1"}},
		"Back":    {Name: "Back", Type: "URLTest", Now: "Loop", All: []string{"Loop", "us-2"}},
		"us-1":    {Name: "us-1", Type: "Vless"},
		"us-2":    {Name: "us-2", Type: "Vmess"},
		"sg-1":    {Name: "sg-1", Type: "Vless"},
		"jp-1":    {Name: "jp-1", Type: "Trojan"},
		"DIRECT":  {Name: "DIRECT", Type: "Direct"},
		"REJECT":  {Name: "REJECT", Type: "Reject"},
	}
	if got := Leaves(all, "YouTube", "PROXY", "Loop"); !reflect.DeepEqual(got, []string{"us-1", "us-2", "sg-1", "jp-1"}) {
		t.Errorf("leaves %v", got)
	}
	if got := Leaves(all, "DIRECT", "missing"); len(got) != 0 {
		t.Errorf("built-in / unknown: %v", got)
	}
	for _, c := range []struct {
		name, typ string
		want      bool
	}{{"DIRECT", "Direct", true}, {"x", "RejectDrop", true}, {"PASS", "", true}, {"DIRECT", "Vless", false}, {"n", "Vless", false}} {
		if IsBuiltin(c.name, c.typ) != c.want {
			t.Errorf("IsBuiltin(%q, %q)", c.name, c.typ)
		}
	}
}
