package extraction

import (
	"bytes"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"github.com/ghislain95/hippo/backend/internal/store"
)

// A YouTube video is extracted from its canonical page, https://www.youtube.com/watch?v=<id>,
// never from the URL captured: its tracking parameters do not leave the NAS. The page is read
// in the child for its player data (readYouTube); when it gives none, oEmbed gives the title
// and the channel (RG-22, RG-23, design.md of extract-youtube).

const (
	// youtubeHost is the host every video is asked from, whatever the form captured.
	youtubeHost = "www.youtube.com"
	// videoIDLength is the length of a YouTube video id.
	videoIDLength = 11
	// playerToken names the player data in the scripts of a watch page.
	playerToken = "ytInitialPlayerResponse"
	// maxPlayerTokens is the number of occurrences of playerToken tried, in order.
	maxPlayerTokens = 3
	// maxVideoSeconds is the longest duration kept as a reading time, about 11 days: beyond, it
	// is not a duration.
	maxVideoSeconds = 1_000_000
	// jsonAccept is the Accept of an oEmbed request.
	jsonAccept = "application/json"
)

// Reason classes of a video (docs/glossary.md).
const (
	// reasonOEmbed: the page gave no metadata, oEmbed gave the title.
	reasonOEmbed = "oembed"
	// reasonNoMetadata: the page answered, but without usable player data.
	reasonNoMetadata = "no_metadata"
)

// formatYouTube is the format of a video page, read for its player data by the child.
const formatYouTube = "youtube"

// videoPathForms are the first path segments of youtube.com whose second segment is a video id.
var videoPathForms = map[string]bool{"shorts": true, "live": true, "embed": true, "v": true}

// videoID returns the id of the video u names: youtu.be/<id>, or on youtube.com and its
// subdomains watch?v=<id> (the first v), /shorts/<id>, /live/<id>, /embed/<id> or /v/<id>. A
// trailing slash and further segments are tolerated, scheme and port ignored. Any other URL,
// on YouTube or not, names no video.
func videoID(u *url.URL) (string, bool) {
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if !isYouTube(host) {
		return "", false
	}
	segments := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	var id string
	switch {
	case host == "youtu.be":
		id = segments[0]
	case segments[0] == "watch":
		if values := u.Query()["v"]; len(values) > 0 {
			id = values[0]
		}
	case videoPathForms[segments[0]] && len(segments) > 1:
		id = segments[1]
	}
	if !isVideoID(id) {
		return "", false
	}
	return id, true
}

// isVideoID tells whether id has the form of a YouTube video id: 11 characters among A-Z,
// a-z, 0-9, - and _.
func isVideoID(id string) bool {
	if len(id) != videoIDLength {
		return false
	}
	for _, c := range []byte(id) {
		switch {
		case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

// canonicalPage is the page of the video id: the only page of a video requested.
func canonicalPage(id string) *url.URL {
	return &url.URL{Scheme: "https", Host: youtubeHost, Path: "/watch", RawQuery: "v=" + id}
}

// oembedURL is the oEmbed answer of the video id, asked for its canonical page.
func oembedURL(id string) *url.URL {
	return &url.URL{
		Scheme: "https", Host: youtubeHost, Path: "/oembed",
		RawQuery: "url=" + url.QueryEscape(canonicalPage(id).String()) + "&format=json",
	}
}

// isYouTube tells whether host is youtube.com, one of its subdomains, or youtu.be.
func isYouTube(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	return host == "youtube.com" || strings.HasSuffix(host, ".youtube.com") || host == "youtu.be"
}

// videoDetails are the fields of the player data kept, each decoded on its own: a field of an
// unexpected type counts as absent without losing the others.
type videoDetails struct {
	VideoID          json.RawMessage `json:"videoId"`
	Title            json.RawMessage `json:"title"`
	Author           json.RawMessage `json:"author"`
	LengthSeconds    json.RawMessage `json:"lengthSeconds"`
	ShortDescription json.RawMessage `json:"shortDescription"`
}

// readYouTube reads the player data of a watch page for the video id: its title, channel,
// description and duration (RG-22). The first block of player data naming id with a title
// wins; none gives the fallback of l, reason no_metadata. Its memory follows the page: it runs
// only in the reading process (RunChild), never in the service.
func readYouTube(body []byte, id string, l link) outcome {
	rest := body
	for range maxPlayerTokens {
		at := bytes.Index(rest, []byte(playerToken))
		if at < 0 {
			break
		}
		rest = rest[at+len(playerToken):]
		block, ok := playerBlock(rest)
		if !ok {
			continue
		}
		if out, ok := videoOutcome(block, id, l); ok {
			return out
		}
	}
	return fallback(reasonNoMetadata, l)
}

// playerBlock returns what follows "= {" after a token, from the brace, when it does.
func playerBlock(rest []byte) ([]byte, bool) {
	rest = bytes.TrimLeft(rest, " \t\r\n")
	if len(rest) == 0 || rest[0] != '=' {
		return nil, false
	}
	rest = bytes.TrimLeft(rest[1:], " \t\r\n")
	if len(rest) == 0 || rest[0] != '{' {
		return nil, false
	}
	return rest, true
}

// videoOutcome decodes the JSON value at the start of block, which ends where it ends (the
// script goes on), and builds the outcome of the video id from its videoDetails, if they name
// it with a title.
func videoOutcome(block []byte, id string, l link) (outcome, bool) {
	var player struct {
		VideoDetails json.RawMessage `json:"videoDetails"`
	}
	// Decode reads one value and ignores what follows; it refuses a nesting beyond 10 000
	// levels and makes invalid UTF-8 U+FFFD.
	if err := json.NewDecoder(bytes.NewReader(block)).Decode(&player); err != nil || player.VideoDetails == nil {
		return outcome{}, false
	}
	var details videoDetails
	if err := json.Unmarshal(player.VideoDetails, &details); err != nil {
		return outcome{}, false
	}
	title := cutTitle(clean(jsonString(details.Title)))
	if !isVideoID(id) || jsonString(details.VideoID) != id || title == "" {
		return outcome{}, false
	}
	description, truncated := truncate(cleanDescription(jsonString(details.ShortDescription)))
	return outcome{Reason: reasonOK, Extraction: store.Extraction{
		Mode:             ModeFallback, // no transcript: the description stands for the content (RG-22, RG-23)
		Title:            &title,
		SiteName:         optional(cutTitle(firstOf(clean(jsonString(details.Author)), l.host))),
		ContentText:      optional(description),
		ContentTruncated: truncated,
		ReadingMinutes:   videoMinutes(details.LengthSeconds),
	}}, true
}

// jsonString is the string raw holds, "" when it holds anything else.
func jsonString(raw json.RawMessage) string {
	var s string
	if raw == nil || json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}

// videoMinutes is the duration of raw, a number of seconds as a JSON string or number, in
// minutes rounded up (RG-25); nil when it is not a decimal integer, 0 or less (a live stream),
// or above maxVideoSeconds.
func videoMinutes(raw json.RawMessage) *int64 {
	var number json.Number // a JSON number, or a string holding one
	if raw == nil || json.Unmarshal(raw, &number) != nil {
		return nil
	}
	seconds, err := strconv.ParseInt(number.String(), 10, 64)
	if err != nil || seconds <= 0 || seconds > maxVideoSeconds {
		return nil
	}
	minutes := (seconds + 59) / 60
	return &minutes
}

// cleanDescription makes a description valid UTF-8 and keeps its line breaks, chapters and
// links on their lines; carriage returns and control characters other than line feed and tab
// are removed.
func cleanDescription(text string) string {
	text = strings.Map(func(r rune) rune {
		if r != '\n' && r != '\t' && unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.ToValidUTF8(text, "\uFFFD"))
	return strings.TrimSpace(text)
}

// oembedOutcome reads an oEmbed answer: its title and author_name only (RG-23). ok is false
// when it holds no title.
func oembedOutcome(body []byte, l link) (outcome, bool) {
	var answer struct {
		Title      json.RawMessage `json:"title"`
		AuthorName json.RawMessage `json:"author_name"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		return outcome{}, false
	}
	title := cutTitle(clean(jsonString(answer.Title)))
	if title == "" {
		return outcome{}, false
	}
	return outcome{Reason: reasonOEmbed, Extraction: store.Extraction{
		Mode:     ModeFallback,
		Title:    &title,
		SiteName: optional(cutTitle(firstOf(clean(jsonString(answer.AuthorName)), l.host))),
	}}, true
}
