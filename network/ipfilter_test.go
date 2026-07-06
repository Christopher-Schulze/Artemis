package network

import (
	"net"
	"net/url"
	"testing"
)

func TestIsPrivateOrLocal(t *testing.T) {
	cases := []struct {
		ip   string
		want bool
	}{
		{"127.0.0.1", true},
		{"::1", true},
		{"10.0.0.1", true},
		{"192.168.1.1", true},
		{"172.16.0.1", true},
		{"169.254.1.1", true},
		{"100.64.0.1", true},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"2606:4700:4700::1111", false},
	}
	for _, tc := range cases {
		got := IsPrivateOrLocal(net.ParseIP(tc.ip))
		if got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.ip, got, tc.want)
		}
	}
}

func TestCheckHostPublicNumeric(t *testing.T) {
	u, _ := url.Parse("http://127.0.0.1:8080/")
	if err := CheckHostPublic(u); err == nil {
		t.Error("loopback should be blocked")
	}
	u, _ = url.Parse("http://1.1.1.1/")
	if err := CheckHostPublic(u); err != nil {
		t.Errorf("public IP rejected: %v", err)
	}
}
