package extraction_test

import (
	"net/netip"
	"testing"

	"github.com/ghislain95/hippo/backend/internal/extraction"
)

func TestPublicAddress(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		// Internet addresses, any port.
		{"93.184.215.14:443", true},
		{"1.1.1.1:80", true},
		{"8.8.8.8:8080", true},
		{"[2606:2800:21f:cb07:6820:80da:af6b:8b2c]:443", true},
		{"[2001:4860:4860::8888]:53", true},

		// Loopback, unspecified, private networks, link-local (RG-27).
		{"127.0.0.1:80", false},
		{"127.1.2.3:8080", false},
		{"0.0.0.0:80", false},
		{"0.1.2.3:80", false},
		{"10.0.0.5:80", false},
		{"172.16.0.1:80", false},
		{"172.31.255.255:80", false},
		{"192.168.1.1:80", false},
		{"169.254.169.254:80", false},
		{"[::1]:80", false},
		{"[::]:80", false},
		{"[fe80::1]:80", false},
		{"[fe80::1%eth0]:80", false},
		{"[2606:4700::1%eth0]:443", false}, // a zone is refused even on a global address
		{"[fc00::1]:80", false},
		{"[fd12:3456:789a::1]:80", false},
		{"[fec0::1]:80", false},

		// Tailscale (CGNAT and its IPv6 range), and other special ranges.
		{"100.64.0.1:80", false},
		{"100.100.100.100:80", false},
		{"100.127.255.254:80", false},
		{"[fd7a:115c:a1e0::1]:80", false},
		{"192.0.0.8:80", false},
		{"198.18.0.1:80", false},
		{"198.19.255.255:80", false},
		{"240.0.0.1:80", false},
		{"255.255.255.255:80", false},
		{"224.0.0.1:80", false},
		{"[ff02::1]:80", false},
		{"[64:ff9b::a00:5]:80", false},
		{"[2002:a00:5::1]:80", false},

		// An IPv4 address mapped in IPv6 is judged as IPv4.
		{"[::ffff:127.0.0.1]:80", false},
		{"[::ffff:192.168.1.1]:80", false},
		{"[::ffff:93.184.215.14]:443", true},

		// Neighbours of the refused ranges stay allowed.
		{"100.63.255.255:80", true},
		{"100.128.0.0:80", true},
		{"198.17.255.255:80", true},
		{"172.32.0.1:80", true},
	}
	for _, tc := range cases {
		t.Run(tc.addr, func(t *testing.T) {
			addr, err := netip.ParseAddrPort(tc.addr)
			if err != nil {
				t.Fatalf("parse %s: %v", tc.addr, err)
			}
			if got := extraction.PublicAddress(addr); got != tc.want {
				t.Errorf("PublicAddress(%s) = %v, want %v", tc.addr, got, tc.want)
			}
		})
	}
}
