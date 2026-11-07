package naming_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ghislain95/hippo/backend/internal/naming"
)

func TestTag(t *testing.T) {
	long := strings.Repeat("abcdefghi-", 4) + "jklmn" // 45 runes
	tests := []struct {
		name, in, want string
	}{
		{"spaces and capitals become a dash and lower case", "Spring Boot", "spring-boot"},
		{"a plain tag is kept", "java", "java"},
		{"accents are removed", "Électronique", "electronique"},
		{"underscores become dashes", "machine_learning", "machine-learning"},
		{"capital accented letters are folded", "ÉCOLE", "ecole"},
		{"a run of separators gives one dash and edges are trimmed", "--a__b  c--", "a-b-c"},
		{"a tag is cut to 40 runes without a trailing dash", long, "abcdefghi-abcdefghi-abcdefghi-abcdefghi"},
		{"a tag cut mid-word keeps 40 runes", strings.Repeat("a", 45), strings.Repeat("a", 40)},
		{"other characters are kept for the schema to refuse", "c++", "c++"},
		{"a ligature without decomposition is kept", "œuvre", "œuvre"},
		{"a digraph letter is lower-cased", "Ǆ", "ǆ"},
		{"tabs and newlines are separators", "a\tb\nc", "a-b-c"},
		{"a tag of separators only is empty", " _- ", ""},
		{"an empty tag stays empty", "", ""},
		{"digits are kept", "Go 1.22", "go-1.22"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := naming.Tag(tt.in)
			if got != tt.want {
				t.Errorf("Tag(%q) = %q, want %q", tt.in, got, tt.want)
			}
			if n := utf8.RuneCountInString(got); n > naming.MaxTagLength {
				t.Errorf("Tag(%q) has %d runes, want at most %d", tt.in, n, naming.MaxTagLength)
			}
		})
	}
}

func TestCategoryKey(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"edges are trimmed and accents removed", "  ménage ", "menage"},
		{"capital accents are folded", "Ménage", "menage"},
		{"inner spaces are collapsed", "Java  &  Spring", "java & spring"},
		{"different words stay different", "Java et Spring", "java et spring"},
		{"capitals are lower-cased", "KUBERNETES", "kubernetes"},
		{"tabs and newlines collapse into one space", "Infra\t&\n DevOps", "infra & devops"},
		{"punctuation is kept", "CI/CD", "ci/cd"},
		{"underscores and dashes are kept", "Auto-hébergement_x", "auto-hebergement_x"},
		{"a ligature without decomposition is kept", "Œuvre", "œuvre"},
		{"a name of spaces only is empty", "   ", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := naming.CategoryKey(tt.in); got != tt.want {
				t.Errorf("CategoryKey(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
