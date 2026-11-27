package kernel

import (
	"os"
	"path/filepath"
	"slices"
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

// GeoItems names the files mihomo loads from its home dir: Country.mmdb
// over geoip.metadb (mihomo prefers it), any case, and nothing else
// (geoip.db is sing-box's, not mihomo's).
func TestGeoItemsMihomoNames(t *testing.T) {
	rel := func(files ...string) []string {
		d := t.TempDir()
		for _, f := range files {
			if err := os.WriteFile(filepath.Join(d, f), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		var out []string
		for _, it := range GeoItems(d, nil) {
			out = append(out, it.Rel)
		}
		return out
	}
	cases := []struct {
		files []string
		want  []string
	}{
		{nil, []string{"GeoIP.dat", "GeoSite.dat", "geoip.metadb", "ASN.mmdb"}},
		{[]string{"geoip.metadb", "country.mmdb"}, []string{"GeoIP.dat", "GeoSite.dat", "country.mmdb", "ASN.mmdb"}},
		{[]string{"geoip.db", "geosite.dat", "asn.mmdb"}, []string{"GeoIP.dat", "geosite.dat", "geoip.metadb", "asn.mmdb"}},
	}
	for _, c := range cases {
		if got := rel(c.files...); !slices.Equal(got, c.want) {
			t.Errorf("%v: got %v, want %v", c.files, got, c.want)
		}
	}
}
