package analysis_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/ghislain95/hippo/backend/internal/analysis"
	"github.com/ghislain95/hippo/backend/internal/analysis/prompt"
	"github.com/ghislain95/hippo/backend/internal/analysis/schema"
	"github.com/ghislain95/hippo/backend/internal/capture/imagetest"
	"github.com/ghislain95/hippo/backend/internal/store"
	"github.com/ghislain95/hippo/backend/internal/store/storetest"
)

// sameJSON tells whether a and b hold the same JSON value.
func sameJSON(t *testing.T, a, b []byte) bool {
	t.Helper()
	var x, y any
	if err := json.Unmarshal(a, &x); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if err := json.Unmarshal(b, &y); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	ja, _ := json.Marshal(x)
	jb, _ := json.Marshal(y)
	return bytes.Equal(ja, jb)
}

// toolSchema is the bundled schema as the tool's input schema, without $schema nor x- keywords.
func toolSchema(t *testing.T) []byte {
	t.Helper()
	s, err := prompt.ToolSchema(schema.ClassifyItem)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// text is the text of the only text block of a request, which comes first.
func (r request) text(t *testing.T) string {
	t.Helper()
	content := r.content(t)
	if len(content) == 0 || content[0].Type != "text" {
		t.Fatalf("message does not start with a text block: %+v", content)
	}
	for _, b := range content[1:] {
		if b.Type == "text" {
			t.Fatal("message has more than one text block")
		}
	}
	return content[0].Text
}

func TestRequest(t *testing.T) {
	full := store.Extraction{Mode: "full", Title: ptr("Un titre de page"), SiteName: ptr("Example Blog"), ContentText: ptr("Le contenu")}

	t.Run("A request carries the tool, the system and the item", func(t *testing.T) {
		e := newEnv(t)
		userID := e.user(t, "owner")
		id := e.link(t, userID, "https://example.com/article", nil, full)
		_, parsed := e.build(t, userID)
		if len(parsed.Requests) != 1 {
			t.Fatalf("%d requests, want 1", len(parsed.Requests))
		}
		r := parsed.of(t, id)
		p := r.Params.parsed
		if r.CustomID != id || p.Model != testModel || p.MaxTokens != 4000 {
			t.Errorf("custom_id %s, model %s, max_tokens %d; want %s, %s, 4000", r.CustomID, p.Model, p.MaxTokens, id, testModel)
		}
		want, err := prompt.Load()
		if err != nil {
			t.Fatal(err)
		}
		if len(p.Tools) != 1 || p.Tools[0].Name != "classify_item" || p.Tools[0].Description != want.ToolDescription ||
			!sameJSON(t, p.Tools[0].InputSchema, toolSchema(t)) {
			t.Errorf("tools are not the single classify_item tool with the bundled schema: %d tools", len(p.Tools))
		}
		if len(p.ToolChoice) != 2 || p.ToolChoice["type"] != "tool" || p.ToolChoice["name"] != "classify_item" {
			t.Errorf("tool_choice = %v, want the forced classify_item tool", p.ToolChoice)
		}
		if len(p.System) != 2 || p.System[0].Type != "text" || p.System[0].Text != want.Fixed || p.System[0].CacheControl != nil ||
			p.System[1].Type != "text" || !strings.HasPrefix(p.System[1].Text, "<referentiel>") ||
			len(p.System[1].CacheControl) != 1 || p.System[1].CacheControl["type"] != "ephemeral" {
			t.Errorf("system is not the fixed part, then the référentiel with a cache point: %+v", p.System)
		}
		if len(p.Messages) != 1 || p.Messages[0].Role != "user" {
			t.Errorf("messages = %+v, want one user message", p.Messages)
		}
		var keys []string
		for key := range r.Params.raw {
			keys = append(keys, key)
		}
		if len(keys) != 6 {
			t.Errorf("params keys = %v, want model, max_tokens, tools, tool_choice, system, messages", keys)
		}
	})

	t.Run("A retried cut response gets the doubled cap", func(t *testing.T) {
		e := newEnv(t)
		userID := e.user(t, "owner")
		id := e.link(t, userID, "https://example.com/long", nil, full)
		other := e.note(t, userID, "Une idée")
		storetest.SetIncompleteRetried(t, e.path, userID, id)
		_, parsed := e.build(t, userID)
		if got := parsed.of(t, id).Params.parsed.MaxTokens; got != 8000 {
			t.Errorf("max_tokens = %d, want 8000", got)
		}
		if got := parsed.of(t, other).Params.parsed.MaxTokens; got != 4000 {
			t.Errorf("max_tokens of another item = %d, want 4000", got)
		}
	})

	t.Run("The référentiel is the same in every request", func(t *testing.T) {
		e := newEnv(t)
		userID := e.user(t, "owner")
		ids := []string{
			e.link(t, userID, "https://example.com/a", nil, full),
			e.note(t, userID, "Une idée"),
		}
		img, _ := e.image(t, userID, imagetest.JPEG(64, 48), "image/jpeg", nil)
		ids = append(ids, img)
		_, parsed := e.build(t, userID)
		first := parsed.of(t, ids[0]).Params.raw
		for _, id := range ids[1:] {
			raw := parsed.of(t, id).Params.raw
			for _, key := range []string{"tools", "tool_choice", "system"} {
				if !bytes.Equal(raw[key], first[key]) {
					t.Errorf("%s of %s differs from the first request's", key, id)
				}
			}
		}
	})

	t.Run("a ULID fits the custom_id pattern", func(t *testing.T) {
		e := newEnv(t)
		pattern := regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
		for range 1000 {
			id, err := e.st.NewID()
			if err != nil {
				t.Fatalf("NewID: %v", err)
			}
			if !pattern.MatchString(id) {
				t.Fatalf("ULID %q does not fit the custom_id pattern", id)
			}
		}
	})
}

func TestItemMessage(t *testing.T) {
	t.Run("A fully extracted link", func(t *testing.T) {
		e := newEnv(t)
		userID := e.user(t, "owner")
		id := e.link(t, userID, "https://example.com/article", ptr("pour le projet X"), store.Extraction{
			Mode: "full", Title: ptr("Un titre de page"), SiteName: ptr("Example Blog"), ContentText: ptr("Le contenu\nsur deux lignes"),
		})
		_, parsed := e.build(t, userID)
		r := parsed.of(t, id)
		want := "<element>\nforme: link\nurl: https://example.com/article\nsite: Example Blog\ntitre_original: Un titre de page\n" +
			"extraction: complète\ncontenu_tronqué: non\nnote_de_capture: pour le projet X\n</element>\n" +
			"<contenu>\nLe contenu\nsur deux lignes\n</contenu>"
		if got := r.text(t); got != want {
			t.Errorf("message =\n%s\nwant\n%s", got, want)
		}
		if len(r.content(t)) != 1 {
			t.Errorf("message of %d blocks, want a single text", len(r.content(t)))
		}
	})

	t.Run("A fallback link without content", func(t *testing.T) {
		e := newEnv(t)
		userID := e.user(t, "owner")
		id := e.link(t, userID, "https://www.Example.org/page", nil, store.Extraction{Mode: "fallback", SiteName: ptr("Example")})
		_, parsed := e.build(t, userID)
		want := "<element>\nforme: link\nurl: https://www.Example.org/page\nsite: Example\n" +
			"extraction: repli sur les métadonnées\ncontenu_tronqué: non\n</element>"
		if got := parsed.of(t, id).text(t); got != want {
			t.Errorf("message =\n%s\nwant\n%s", got, want)
		}
	})

	t.Run("A truncated content is flagged", func(t *testing.T) {
		e := newEnv(t)
		userID := e.user(t, "owner")
		id := e.link(t, userID, "https://example.com/long", nil, store.Extraction{
			Mode: "full", ContentText: ptr("Le début"), ContentTruncated: true,
		})
		_, parsed := e.build(t, userID)
		if got := parsed.of(t, id).text(t); !strings.Contains(got, "\ncontenu_tronqué: oui\n") {
			t.Errorf("message =\n%s\nwant contenu_tronqué: oui", got)
		}
	})

	t.Run("Credentials of a URL are not sent", func(t *testing.T) {
		e := newEnv(t)
		userID := e.user(t, "owner")
		id := e.link(t, userID, "https://user:secret@example.com/page", nil, store.Extraction{Mode: "full", ContentText: ptr("Texte")})
		raw, parsed := e.build(t, userID)
		if got := parsed.of(t, id).text(t); !strings.Contains(got, "\nurl: https://example.com/page\n") {
			t.Errorf("message =\n%s\nwant url: https://example.com/page", got)
		}
		if bytes.Contains(raw, []byte("secret")) {
			t.Error("the body contains the password of the URL")
		}
	})

	t.Run("A note", func(t *testing.T) {
		e := newEnv(t)
		userID := e.user(t, "owner")
		id := e.note(t, userID, "Première ligne\nseconde ligne")
		_, parsed := e.build(t, userID)
		want := "<element>\nforme: note\nextraction: aucune\n</element>\n<contenu>\nPremière ligne\nseconde ligne\n</contenu>"
		if got := parsed.of(t, id).text(t); got != want {
			t.Errorf("message =\n%s\nwant\n%s", got, want)
		}
	})

	t.Run("An image with a capture note", func(t *testing.T) {
		e := newEnv(t)
		userID := e.user(t, "owner")
		id, _ := e.image(t, userID, imagetest.JPEG(64, 48), "image/jpeg", ptr("le plat du midi\nà refaire"))
		_, parsed := e.build(t, userID)
		r := parsed.of(t, id)
		content := r.content(t)
		want := "<element>\nforme: image\nextraction: aucune\nnote_de_capture: le plat du midi\nà refaire\n</element>"
		if len(content) != 2 || content[0].Type != "text" || content[0].Text != want || content[1].Type != "image" {
			t.Fatalf("message = %+v, want the text\n%s\nthen one image block", content, want)
		}
		if images := r.images(t); len(images) != 1 || !bytes.HasPrefix(images[0], []byte{0xFF, 0xD8}) {
			t.Error("the image block does not hold a JPEG")
		}
	})

	t.Run("a title equal to the host of the URL is not sent", func(t *testing.T) {
		e := newEnv(t)
		userID := e.user(t, "owner")
		id := e.link(t, userID, "https://Example.com:8443/page", nil, store.Extraction{Mode: "full", ContentText: ptr("Texte")})
		_, parsed := e.build(t, userID)
		if got := parsed.of(t, id).text(t); strings.Contains(got, "titre_original") {
			t.Errorf("message =\n%s\nwant no titre_original", got)
		}
	})

	t.Run("a line break in a site or a title does not split its line", func(t *testing.T) {
		e := newEnv(t)
		userID := e.user(t, "owner")
		id := e.link(t, userID, "https://example.com/page", nil, store.Extraction{
			Mode: "full", Title: ptr("Un titre\nsur deux lignes"), SiteName: ptr("Example\r\nBlog"),
		})
		_, parsed := e.build(t, userID)
		got := parsed.of(t, id).text(t)
		if !strings.Contains(got, "\nsite: Example Blog\ntitre_original: Un titre sur deux lignes\n") {
			t.Errorf("message =\n%s\nwant site and titre_original on one line each", got)
		}
	})
}

func TestBody(t *testing.T) {
	ctx := context.Background()

	t.Run("the body holds only the requests of the ids asked, in their order", func(t *testing.T) {
		e := newEnv(t)
		userID := e.user(t, "owner")
		a, b, c := e.note(t, userID, "a"), e.note(t, userID, "b"), e.note(t, userID, "c")
		queue, err := e.st.AnalysisQueue(ctx, userID, analysis.MaxBatchItems)
		if err != nil {
			t.Fatal(err)
		}
		reqs := e.requests(t, userID, 0)
		for _, item := range queue {
			if added, err := reqs.Add(ctx, item); err != nil || !added {
				t.Fatalf("Add = %v, %v", added, err)
			}
		}
		raw, parsed := assemble(t, reqs, []string{a, c})
		if len(parsed.Requests) != 2 || parsed.Requests[0].CustomID != a || parsed.Requests[1].CustomID != c {
			t.Errorf("body holds %d requests, want those of a and c", len(parsed.Requests))
		}
		if bytes.Contains(raw, []byte(b)) {
			t.Error("the body holds the id of an item not asked for")
		}
		if !bytes.HasPrefix(raw, []byte(`{"requests":[{"custom_id":`)) || !bytes.HasSuffix(raw, []byte("}]}")) {
			t.Errorf("body starts %.40q, ends %q", raw, raw[len(raw)-3:])
		}
		if _, err := reqs.WriteBody(&bytes.Buffer{}, []string{"01JB8Z6Q4M1X2Y3Z4A5B6C7D90"}); err == nil {
			t.Error("WriteBody accepted an id without request")
		}
	})

	t.Run("a request that would pass the body limit is not added and the segments stay whole", func(t *testing.T) {
		e := newEnv(t)
		userID := e.user(t, "owner")
		e.note(t, userID, "a")
		e.note(t, userID, "b")
		e.note(t, userID, strings.Repeat("c", 4000))
		queue, err := e.st.AnalysisQueue(ctx, userID, analysis.MaxBatchItems)
		if err != nil {
			t.Fatal(err)
		}
		// Measure the body of the first two, then allow exactly that much.
		probe := e.requests(t, userID, 0)
		for _, item := range queue[:2] {
			if _, err := probe.Add(ctx, item); err != nil {
				t.Fatal(err)
			}
		}
		limit, err := probe.WriteBody(&bytes.Buffer{}, probe.IDs())
		if err != nil {
			t.Fatal(err)
		}

		reqs := e.requests(t, userID, limit)
		var added []bool
		for _, item := range queue {
			ok, err := reqs.Add(ctx, item)
			if err != nil {
				t.Fatalf("Add: %v", err)
			}
			added = append(added, ok)
		}
		if !added[0] || !added[1] || added[2] {
			t.Fatalf("added = %v, want the first two only", added)
		}
		raw, parsed := assemble(t, reqs, reqs.IDs())
		if int64(len(raw)) != limit || len(parsed.Requests) != 2 {
			t.Errorf("body of %d bytes and %d requests, want %d bytes and 2", len(raw), len(parsed.Requests), limit)
		}
	})

	t.Run("a link deleted after the selection is skipped and the next request is written", func(t *testing.T) {
		e := newEnv(t)
		userID := e.user(t, "owner")
		gone := e.link(t, userID, "https://example.com/gone", nil, store.Extraction{Mode: "full", ContentText: ptr("Texte supprimé")})
		next := e.note(t, userID, "suivante")
		queue, err := e.st.AnalysisQueue(ctx, userID, analysis.MaxBatchItems)
		if err != nil || len(queue) != 2 {
			t.Fatalf("AnalysisQueue = %d items, %v", len(queue), err)
		}
		storetest.DeleteItem(t, e.path, userID, gone)
		reqs := e.requests(t, userID, 0)
		added, err := reqs.Add(ctx, queue[0])
		if added || !errors.Is(err, analysis.ErrItemGone) {
			t.Fatalf("Add(deleted) = %v, %v; want ErrItemGone", added, err)
		}
		if size, err := analysis.SegmentFileSize(reqs); err != nil || size != 0 {
			t.Errorf("segment file of %d bytes (%v) after the deleted item, want empty", size, err)
		}
		if added, err := reqs.Add(ctx, queue[1]); err != nil || !added {
			t.Fatalf("Add(next) = %v, %v; want added", added, err)
		}
		raw, parsed := assemble(t, reqs, reqs.IDs())
		if len(parsed.Requests) != 1 || parsed.Requests[0].CustomID != next {
			t.Errorf("body holds %d requests, want the next item's only", len(parsed.Requests))
		}
		if bytes.Contains(raw, []byte(gone)) || bytes.Contains(raw, []byte("Texte supprimé")) {
			t.Error("the body holds the deleted item")
		}
	})

	t.Run("a limit inside the data of an image keeps the requests written whole", func(t *testing.T) {
		e := newEnv(t)
		userID := e.user(t, "owner")
		first := e.note(t, userID, "a")
		e.image(t, userID, imagetest.QuadrantJPEG(640, 480), "image/jpeg", nil)
		queue, err := e.st.AnalysisQueue(ctx, userID, analysis.MaxBatchItems)
		if err != nil {
			t.Fatal(err)
		}
		probe := e.requests(t, userID, 0)
		for _, item := range queue {
			if added, err := probe.Add(ctx, item); err != nil || !added {
				t.Fatalf("Add = %v, %v", added, err)
			}
		}
		noteOnly, err := probe.WriteBody(&bytes.Buffer{}, []string{first})
		if err != nil {
			t.Fatal(err)
		}
		both, err := probe.WriteBody(&bytes.Buffer{}, probe.IDs())
		if err != nil {
			t.Fatal(err)
		}
		segment, err := analysis.SegmentFileSize(probe)
		if err != nil {
			t.Fatal(err)
		}
		const cut = 200 // bytes before the end of the image's request: inside its base64 data
		if both-noteOnly < 4*cut {
			t.Fatalf("image request of %d bytes: too small for the test", both-noteOnly)
		}

		reqs := e.requests(t, userID, both-cut)
		if added, err := reqs.Add(ctx, queue[0]); err != nil || !added {
			t.Fatalf("Add(note) = %v, %v", added, err)
		}
		if added, err := reqs.Add(ctx, queue[1]); err != nil || added {
			t.Fatalf("Add(image) = %v, %v; want not added", added, err)
		}
		size, err := analysis.SegmentFileSize(reqs)
		if err != nil || size != segment-(both-noteOnly-1) {
			t.Errorf("segment file of %d bytes (%v), want %d: the note's request only", size, err, segment-(both-noteOnly-1))
		}
		raw, parsed := assemble(t, reqs, reqs.IDs())
		if int64(len(raw)) != noteOnly || len(parsed.Requests) != 1 || parsed.Requests[0].CustomID != first {
			t.Errorf("body of %d bytes and %d requests, want the note's request only (%d bytes)", len(raw), len(parsed.Requests), noteOnly)
		}
	})

	t.Run("an image without copy whose request does not fit is neither logged nor counted", func(t *testing.T) {
		e := newEnv(t)
		userID := e.user(t, "owner")
		first := e.note(t, userID, "a")
		e.image(t, userID, imagetest.JPEGDeclaring(10000, 6000), "image/jpeg", nil)
		queue, err := e.st.AnalysisQueue(ctx, userID, analysis.MaxBatchItems)
		if err != nil {
			t.Fatal(err)
		}
		probe := e.requests(t, userID, 0)
		if _, err := probe.Add(ctx, queue[0]); err != nil {
			t.Fatal(err)
		}
		limit, err := probe.WriteBody(&bytes.Buffer{}, []string{first})
		if err != nil {
			t.Fatal(err)
		}
		reqs := e.requests(t, userID, limit)
		if added, err := reqs.Add(ctx, queue[0]); err != nil || !added {
			t.Fatalf("Add(note) = %v, %v", added, err)
		}
		if added, err := reqs.Add(ctx, queue[1]); err != nil || added {
			t.Fatalf("Add(image) = %v, %v; want not added", added, err)
		}
		if n := reqs.ImagesWithoutCopy(); n != 0 {
			t.Errorf("ImagesWithoutCopy = %d, want 0", n)
		}
		if e.hasLog(t, map[string]string{"reason": "too_many_pixels"}) {
			t.Errorf("an image of a request not kept was logged:\n%s", e.logs)
		}
	})

	t.Run("a closed request writer refuses to add", func(t *testing.T) {
		e := newEnv(t)
		userID := e.user(t, "owner")
		e.note(t, userID, "a")
		queue, err := e.st.AnalysisQueue(ctx, userID, 1)
		if err != nil {
			t.Fatal(err)
		}
		reqs := e.requests(t, userID, 0)
		if err := reqs.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := reqs.Add(ctx, queue[0]); err == nil {
			t.Error("Add after Close succeeded")
		}
	})
}

func TestItemMessageEncoding(t *testing.T) {
	t.Run("credentials are cut and the rest of the URL is sent byte for byte", func(t *testing.T) {
		cases := map[string]string{
			"https://user:secret@example.com/a%2Fb?q=%7e&r=1#":     "https://example.com/a%2Fb?q=%7e&r=1#",
			"https://user@Example.com:8443/p?x=a@b#frag@ment":      "https://Example.com:8443/p?x=a@b#frag@ment",
			"http://u%40v:p%3Aw@example.com":                       "http://example.com",
			"https://user:secret@example.com?next=/x":              "https://example.com?next=/x",
			"https://example.com/no/credentials?keep=%7E#Fragment": "https://example.com/no/credentials?keep=%7E#Fragment",
		}
		for captured, want := range cases {
			e := newEnv(t)
			userID := e.user(t, "owner")
			id := e.link(t, userID, captured, nil, store.Extraction{Mode: "fallback"})
			raw, parsed := e.build(t, userID)
			if got := parsed.of(t, id).text(t); !strings.Contains(got, "\nurl: "+want+"\n") {
				t.Errorf("%s: message =\n%s\nwant url: %s", captured, got, want)
			}
			if bytes.Contains(raw, []byte("secret")) || bytes.Contains(raw, []byte("p%3Aw")) {
				t.Errorf("%s: the body holds the credentials", captured)
			}
		}
	})

	t.Run("quotes, backslashes, control characters, invalid UTF-8 and line separators stay valid JSON", func(t *testing.T) {
		e := newEnv(t)
		userID := e.user(t, "owner")
		title := "Il a dit \"oui\" \\ puis\x01 \xff et\u2028fin"
		note := "Note \"citée\" \\ \x02\u2029 \xfe"
		content := "<contenu> \"brut\" \\ \x1f \u2028 \xc3"
		id := e.link(t, userID, `https://example.com/p?q="x"`, &note, store.Extraction{
			Mode: "full", Title: &title, SiteName: ptr("Site \"A\""), ContentText: &content,
		})
		raw, parsed := e.build(t, userID) // fails the test unless the body parses
		valid := strings.ToValidUTF8
		want := "<element>\nforme: link\nurl: https://example.com/p?q=\"x\"\nsite: Site \"A\"\n" +
			"titre_original: " + valid(title, "\uFFFD") + "\nextraction: complète\ncontenu_tronqué: non\n" +
			"note_de_capture: " + valid(note, "\uFFFD") + "\n</element>\n<contenu>\n" + valid(content, "\uFFFD") + "\n</contenu>"
		if got := parsed.of(t, id).text(t); got != want {
			t.Errorf("message =\n%q\nwant\n%q", got, want)
		}
		if !json.Valid(raw) {
			t.Error("the body is not valid JSON")
		}
	})
}
