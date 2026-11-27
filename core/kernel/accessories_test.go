package kernel

import (
	"os"
	"path/filepath"
	"testing"
)

// A UI dir stamped before the rename keeps its stamp.
func TestReadUIStampLegacy(t *testing.T) {
	d := t.TempDir()
	if err := os.MkdirAll(filepath.Join(d, "ui"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "ui", LegacyUIStamp), []byte(`{"url":"https://x/dist.zip","tag":"v1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := os.OpenRoot(d)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	s, ok := ReadUIStamp(r, "ui")
	if !ok || s.Tag != "v1" {
		t.Errorf("%+v %v", s, ok)
	}
}
