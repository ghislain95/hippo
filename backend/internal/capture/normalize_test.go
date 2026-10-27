package capture_test

import (
	"testing"

	"github.com/ghislain95/hippo/backend/internal/capture"
)

func TestNormalize(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		// Fragment.
		{"fragment removed", "https://example.com/a#section-2", "https://example.com/a"},
		{"empty fragment removed", "https://example.com/a#", "https://example.com/a"},
		{"fragment before a question mark", "https://example.com/a#x?utm_source=y", "https://example.com/a"},
		{"fragment after the query", "https://example.com/a?id=3#top", "https://example.com/a?id=3"},

		// Scheme and host case.
		{"scheme and host lower-cased", "HTTPS://Example.COM/a", "https://example.com/a"},
		{"path case kept", "https://example.com/A/b", "https://example.com/A/b"},
		{"user info kept as sent", "https://User:Pass@Example.com/a", "https://User:Pass@example.com/a"},
		{"port kept", "https://Example.com:8443/a", "https://example.com:8443/a"},
		{"default port kept", "https://example.com:443/a", "https://example.com:443/a"},
		{"IPv6 host lower-cased, port kept", "http://[FE80::1]:8080/a", "http://[fe80::1]:8080/a"},
		{"internationalised host lower-cased, not converted", "https://BÜCHER.example/a", "https://bücher.example/a"},
		{"www kept", "https://www.example.com/a", "https://www.example.com/a"},
		{"http kept", "http://example.com/a", "http://example.com/a"},

		// Trailing slash.
		{"root slash removed", "https://example.com/", "https://example.com"},
		{"no path", "https://example.com", "https://example.com"},
		{"trailing slash removed", "https://example.com/docs/page/", "https://example.com/docs/page"},
		{"one trailing slash only", "https://example.com/a//", "https://example.com/a/"},
		{"internal double slash kept", "https://example.com/a//b", "https://example.com/a//b"},
		{"encoded slash kept", "https://example.com/a%2Fb%2F", "https://example.com/a%2Fb%2F"},
		{"trailing slash before the query", "https://example.com/list/?x=1", "https://example.com/list?x=1"},

		// Query.
		{"empty query", "https://example.com/a?", "https://example.com/a"},
		{"root with empty query", "https://example.com/?", "https://example.com"},
		{"empty pairs removed", "https://example.com/a?&x=1&&y=2&", "https://example.com/a?x=1&y=2"},
		{"utm parameters removed", "https://example.com/a?utm_source=x&id=3&utm_campaign=z", "https://example.com/a?id=3"},
		{"tracking names in any case", "https://example.com/a?UTM_Medium=y&FBCLID=1&GClid=2&Igsh=3&SI=4&Ref=5&id=3", "https://example.com/a?id=3"},
		{"fbclid gclid igsh si removed", "https://example.com/a?fbclid=1&gclid=2&igsh=3&si=4", "https://example.com/a"},
		{"ref without value removed", "https://example.com/a?ref&id=3", "https://example.com/a?id=3"},
		{"ref with empty value removed", "https://example.com/a?ref=&id=3", "https://example.com/a?id=3"},
		{"utm without underscore kept", "https://example.com/a?utm=1", "https://example.com/a?utm=1"},
		{"name starting like a tracking name kept", "https://example.com/a?referrer=x&site=y", "https://example.com/a?referrer=x&site=y"},
		{"pairs sorted by name", "https://example.com/search?b=2&a=1", "https://example.com/search?a=1&b=2"},
		{"repeated names keep their order", "https://example.com/list?x=1&tag=b&tag=a", "https://example.com/list?tag=b&tag=a&x=1"},
		{"sorted by raw name, value ignored", "https://example.com/a?b=1&a=9&a=2", "https://example.com/a?a=9&a=2&b=1"},
		{"percent-encoding untouched", "https://example.com/a?q=caf%C3%A9&p=a+b&r=%2f", "https://example.com/a?p=a+b&q=caf%C3%A9&r=%2f"},
		{"semicolon is not a separator", "https://example.com/a?b=1;a=2", "https://example.com/a?b=1;a=2"},
		{"only tracking parameters", "https://example.com/a/?utm_source=x", "https://example.com/a"},

		// Delta scenarios.
		{"scenario tracking parameters", "https://example.com/article?utm_source=x&id=3&fbclid=abc&UTM_Medium=y", "https://example.com/article?id=3"},
		{"scenario fragment, trailing slash and host case", "HTTPS://Example.COM/docs/page/#section-2", "https://example.com/docs/page"},
		{"scenario repeated parameters", "https://example.com/list/?x=1&&tag=b&ref=home&tag=a", "https://example.com/list?tag=b&tag=a&x=1"},
		{"scenario parameter order", "https://example.com/search?a=1&b=2", "https://example.com/search?a=1&b=2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := capture.Normalize(tc.in); got != tc.want {
				t.Errorf("Normalize(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizeKeepsDifferences(t *testing.T) {
	first := capture.Normalize("https://example.com/a")
	for _, other := range []string{
		"http://example.com/a", "https://www.example.com/a", "https://example.com:443/a",
		"https://example.com/A", "https://example.com/a?id=4",
	} {
		if got := capture.Normalize(other); got == first {
			t.Errorf("Normalize(%q) = %q, the same as https://example.com/a", other, got)
		}
	}
}
