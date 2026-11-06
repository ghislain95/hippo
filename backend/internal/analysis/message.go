package analysis

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/ghislain95/hippo/backend/internal/capture"
	"github.com/ghislain95/hippo/backend/internal/store"
)

// Labels of the extraction line of the <element> block (docs/prompt/system-prompt.md).
var extractionLabels = map[string]string{
	"full":     "complète",
	"fallback": "repli sur les métadonnées",
	"none":     "aucune",
}

// noExtraction is the extraction label of a note and of an image.
const noExtraction = "aucune"

// elementText returns the text block of the user message of item (design.md D5): the <element>
// block, its lines in the order of the prompt contract, absent fields left out; then, for a
// link with content or for a note, the <contenu> block. content is the extracted content of a
// link, nil when it has none; it is not read for a note nor an image.
//
//   - link: forme, url (without its user info), site, titre_original (unless it is still the
//     provisional title, the host, RG-26), extraction, contenu_tronqué, note_de_capture;
//   - note: forme, extraction "aucune"; the note text is the content;
//   - image: forme, extraction "aucune", note_de_capture; its image follows the text block.
//
// The site and the title are written on one line; the note de capture stays as written, last
// line of the block. Nothing is escaped: the prompt treats the block as data.
func elementText(item store.AnalysisQueueItem, content *string) (string, error) {
	var b strings.Builder
	line := func(key, value string) {
		b.WriteString(key)
		b.WriteString(": ")
		b.WriteString(value)
		b.WriteByte('\n')
	}
	captureNote := func() {
		if item.UserNote != nil && *item.UserNote != "" {
			line("note_de_capture", *item.UserNote)
		}
	}
	var body *string
	b.WriteString("<element>\n")
	switch item.Kind {
	case capture.KindLink:
		line("forme", capture.KindLink)
		if item.URL != nil {
			if clean, ok := withoutUserInfo(*item.URL); ok {
				line("url", clean)
			}
		}
		if item.SiteName != nil && strings.TrimSpace(*item.SiteName) != "" {
			line("site", oneLine.Replace(*item.SiteName))
		}
		if originalTitle(item) {
			line("titre_original", oneLine.Replace(item.Title))
		}
		if item.ExtractionMode != nil {
			if label, ok := extractionLabels[*item.ExtractionMode]; ok {
				line("extraction", label)
			}
		}
		line("contenu_tronqué", map[bool]string{true: "oui", false: "non"}[item.ContentTruncated])
		captureNote()
		body = content
	case capture.KindNote:
		line("forme", capture.KindNote)
		line("extraction", noExtraction)
		body = item.UserNote
	case capture.KindImage:
		line("forme", capture.KindImage)
		line("extraction", noExtraction)
		captureNote()
	default:
		// Fail closed: a form the prompt does not describe is not sent.
		return "", fmt.Errorf("item message: unknown kind %q", item.Kind)
	}
	b.WriteString("</element>")
	if body != nil && *body != "" {
		b.WriteString("\n<contenu>\n")
		b.WriteString(*body)
		b.WriteString("\n</contenu>")
	}
	return b.String(), nil
}

// originalTitle tells whether the title of a link is worth sending: not empty, and no longer
// its provisional title, the host of its URL (RG-26).
func originalTitle(item store.AnalysisQueueItem) bool {
	if strings.TrimSpace(item.Title) == "" {
		return false
	}
	if item.URL == nil {
		return true
	}
	provisional, ok := capture.ProvisionalTitle(*item.URL)
	return !ok || item.Title != provisional
}

// withoutUserInfo returns the captured URL rawURL without its user info (user:password@), as
// the extraction does before its request. Only that segment is cut from the string: the rest is
// sent byte for byte as captured, never re-serialised nor normalised. ok is false when rawURL
// does not parse, or when the user info cannot be cut cleanly: the URL is then not sent at all.
func withoutUserInfo(rawURL string) (string, bool) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", false
	}
	if u.User == nil {
		return rawURL, true
	}
	// As url.Parse reads it: the authority follows "//" and ends at the first "/", "?" or "#";
	// the user info ends at its last "@".
	slashes := strings.Index(rawURL, "//")
	if slashes < 0 {
		return "", false
	}
	start := slashes + 2
	end := len(rawURL)
	if i := strings.IndexAny(rawURL[start:], "/?#"); i >= 0 {
		end = start + i
	}
	at := strings.LastIndex(rawURL[start:end], "@")
	if at < 0 {
		return "", false
	}
	clean := rawURL[:start] + rawURL[start+at+1:]
	if parsed, err := url.Parse(clean); err != nil || parsed.User != nil {
		return "", false // fail closed: never send what may still hold credentials
	}
	return clean, true
}
