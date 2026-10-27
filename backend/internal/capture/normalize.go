package capture

import (
	"slices"
	"strings"
)

// Normalize returns the normalised form of a link, the key of deduplication (RG-04, RG-05):
// scheme and host in lower case, fragment removed, empty and tracking query pairs removed, one
// trailing slash of the path removed, remaining pairs sorted by name. rawURL is a trimmed URL
// that url.Parse accepted as an absolute http or https URL with a host.
//
// It works on the string as sent: url.URL.String, url.ParseQuery and url.Values.Encode
// re-encode the URL and drop pairs, which would merge or split links behind the user's back.
// User info, port, percent-encoding and path case are kept; an internationalised host is
// lower-cased, never converted to punycode.
func Normalize(rawURL string) string {
	rest, _, _ := strings.Cut(rawURL, "#")
	rest, query, hasQuery := strings.Cut(rest, "?")

	scheme, afterScheme, ok := strings.Cut(rest, "://")
	if !ok {
		// Unreachable for a URL validated by provisionalTitle; kept total rather than panicking.
		return rawURL
	}
	authority, path := afterScheme, ""
	if slash := strings.IndexByte(afterScheme, '/'); slash >= 0 {
		authority, path = afterScheme[:slash], afterScheme[slash:]
	}
	path = strings.TrimSuffix(path, "/")

	var b strings.Builder
	b.WriteString(strings.ToLower(scheme))
	b.WriteString("://")
	b.WriteString(lowerHost(authority))
	b.WriteString(path)
	if hasQuery {
		if kept := normalizeQuery(query); kept != "" {
			b.WriteByte('?')
			b.WriteString(kept)
		}
	}
	return b.String()
}

// lowerHost lower-cases the host of an authority (userinfo@host:port), leaving the user info
// and the port as sent. Like url.Parse, the user info ends at the last "@".
func lowerHost(authority string) string {
	userInfo, hostPort := "", authority
	if at := strings.LastIndexByte(authority, '@'); at >= 0 {
		userInfo, hostPort = authority[:at+1], authority[at+1:]
	}
	host, port := hostPort, ""
	if strings.HasPrefix(hostPort, "[") {
		// IPv6 literal: the port, if any, follows the closing bracket.
		if end := strings.IndexByte(hostPort, ']'); end >= 0 {
			host, port = hostPort[:end+1], hostPort[end+1:]
		}
	} else if colon := strings.LastIndexByte(hostPort, ':'); colon >= 0 {
		host, port = hostPort[:colon], hostPort[colon:]
	}
	return userInfo + strings.ToLower(host) + port
}

// normalizeQuery drops the empty and tracking pairs of a raw query and sorts the others by raw
// name, values of a same name keeping their order.
func normalizeQuery(query string) string {
	type pair struct{ name, raw string }
	var pairs []pair
	for raw := range strings.SplitSeq(query, "&") {
		if raw == "" {
			continue
		}
		name, _, _ := strings.Cut(raw, "=")
		if isTracking(name) {
			continue
		}
		pairs = append(pairs, pair{name: name, raw: raw})
	}
	slices.SortStableFunc(pairs, func(a, b pair) int { return strings.Compare(a.name, b.name) })
	kept := make([]string, len(pairs))
	for i, p := range pairs {
		kept[i] = p.raw
	}
	return strings.Join(kept, "&")
}

// isTracking tells whether a query parameter is one RG-05 removes, in any case.
func isTracking(name string) bool {
	lower := strings.ToLower(name)
	switch lower {
	case "fbclid", "gclid", "igsh", "si", "ref":
		return true
	default:
		return strings.HasPrefix(lower, "utm_")
	}
}
