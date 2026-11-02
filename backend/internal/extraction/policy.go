package extraction

import "net/netip"

// refusedPrefixes are the special ranges that netip does not already classify as private,
// loopback, link-local, multicast or unspecified (RG-27).
var refusedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),     // "this network"
	netip.MustParsePrefix("100.64.0.0/10"), // shared address space: Tailscale, CGNAT
	netip.MustParsePrefix("192.0.0.0/24"),  // IETF protocol assignments
	netip.MustParsePrefix("198.18.0.0/15"), // benchmarking
	netip.MustParsePrefix("240.0.0.0/4"),   // reserved, and the broadcast address
	netip.MustParsePrefix("64:ff9b::/96"),  // NAT64: would reach any IPv4 address, private ones too
	netip.MustParsePrefix("2002::/16"),     // 6to4: same
	netip.MustParsePrefix("fec0::/10"),     // deprecated site-local
}

// PublicAddress is the address policy of production (RG-27): it allows an Internet address
// only, on any port. Loopback, private networks, link-local, unspecified, multicast, the
// Tailscale range, IPv6 unique-local and site-local ranges, and any address with an IPv6 zone
// are refused. An IPv4 address mapped in IPv6 is judged as IPv4.
func PublicAddress(addr netip.AddrPort) bool {
	ip := addr.Addr().Unmap()
	if ip.Zone() != "" || !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return false
	}
	for _, prefix := range refusedPrefixes {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}
