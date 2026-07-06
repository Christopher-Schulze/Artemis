package security

import (
	"net"
	"testing"
)

func TestIsPrivateIP198_18(t *testing.T) {
	// 198.18.0.0/15 (benchmark testing range, spec L4199)
	cases := []struct {
		ip   string
		want bool
	}{
		{"198.18.0.1", true},
		{"198.19.255.255", true},
		{"198.17.0.1", false},
		{"198.20.0.1", false},
	}
	for _, c := range cases {
		t.Run(c.ip, func(t *testing.T) {
			got := IsPrivateIP(parseIP(c.ip))
			if got != c.want {
				t.Errorf("IsPrivateIP(%s) = %v, want %v", c.ip, got, c.want)
			}
		})
	}
}

func TestIsPrivateIPRanges(t *testing.T) {
	cases := []struct {
		ip   string
		want bool
	}{
		{"10.0.0.1", true},       // 10/8
		{"172.16.0.1", true},     // 172.16/12
		{"172.31.255.255", true}, // 172.16/12 upper
		{"172.32.0.1", false},    // outside 172.16/12
		{"192.168.1.1", true},    // 192.168/16
		{"127.0.0.1", true},      // loopback
		{"169.254.1.1", true},    // link-local
		{"0.0.0.0", true},        // 0/8
		{"100.64.0.1", true},     // CGNAT
		{"100.128.0.1", false},   // outside CGNAT
		{"8.8.8.8", false},       // public
		{"1.1.1.1", false},       // public
	}
	for _, c := range cases {
		t.Run(c.ip, func(t *testing.T) {
			got := IsPrivateIP(parseIP(c.ip))
			if got != c.want {
				t.Errorf("IsPrivateIP(%s) = %v, want %v", c.ip, got, c.want)
			}
		})
	}
}

func parseIP(s string) net.IP {
	return net.ParseIP(s)
}
