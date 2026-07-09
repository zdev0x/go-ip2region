package ip2region

import "testing"

func TestParseRegion(t *testing.T) {
	cases := []struct {
		name     string
		ip       string
		raw      string
		country  string
		province string
		city     string
		isp      string
		code     string
	}{
		{"full", "1.2.3.4", "中国|广东|深圳|电信|CN", "中国", "广东", "深圳", "电信", "CN"},
		{"zero placeholder", "1.1.1.1", "中国|0|0|0|0", "中国", "", "", "", ""},
		{"short", "8.8.8.8", "美国|0|0|0|US", "美国", "", "", "", "US"},
		{"empty", "0.0.0.0", "0|0|0|0|0", "", "", "", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := parseRegion(c.ip, c.raw)
			if r.IP != c.ip || r.Country != c.country || r.Province != c.province ||
				r.City != c.city || r.ISP != c.isp || r.Code != c.code {
				t.Fatalf("got %+v", r)
			}
			if r.Raw != c.raw {
				t.Fatalf("raw mismatch: %q", r.Raw)
			}
		})
	}
}
