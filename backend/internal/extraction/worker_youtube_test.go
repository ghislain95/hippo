package extraction_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ghislain95/hippo/backend/internal/extraction"
	"github.com/ghislain95/hippo/backend/internal/extraction/pagestest"
	"github.com/ghislain95/hippo/backend/internal/store"
)

const (
	// videoA is the fictional video of the scenarios; videoB another one.
	videoA = "aaaaaaaaaa1"
	videoB = "bbbbbbbbbb2"
	// watchA is the canonical page of videoA, and the request the web records for it.
	watchA      = "https://www.youtube.com/watch?v=" + videoA
	watchAsked  = "www.youtube.com/watch?v=" + videoA
	oembedAsked = "www.youtube.com/oembed?url=https%3A%2F%2Fwww.youtube.com%2Fwatch%3Fv%3D" + videoA + "&format=json"
)

// oembedJSON is a fictional oEmbed answer of the video id, in the shape of YouTube's.
func oembedJSON(id, title, author string) []byte {
	out, err := json.Marshal(map[string]any{
		"title": title, "author_name": author, "author_url": "https://www.youtube.com/@exampletalks",
		"type": "video", "height": 113, "width": 200, "version": "1.0",
		"provider_name": "YouTube", "provider_url": "https://www.youtube.com/",
		"thumbnail_height": 360, "thumbnail_width": 480,
		"thumbnail_url": "https://i.ytimg.example/vi/" + id + "/hqdefault.jpg",
		"html":          `<iframe width="200" height="113" src="https://www.youtube.com/embed/` + id + `?feature=oembed"></iframe>`,
	})
	if err != nil {
		panic(err)
	}
	return out
}

// multilineDescription is a description of exactly 900 characters on 15 lines, as a video's
// chapters and links.
func multilineDescription() string {
	lines := make([]string, 15)
	for i := range lines {
		words, _ := pagestest.Words(60)
		lines[i] = fmt.Sprintf("%02d:00 %s", i, words)[:59]
	}
	lines[14] += "."
	description := strings.Join(lines, "\n")
	if len(description) != 900 {
		panic(fmt.Sprintf("description of %d characters", len(description)))
	}
	return description
}

// tuningPage is the watch page of videoA with the player data of video.
func tuningPage(video pagestest.Video) []byte {
	return pagestest.VideoPage(video.Title, pagestest.PlayerScript(pagestest.PlayerResponse(pagestest.Details(video), 10)))
}

// tuningTalk is the fictional video of the scenarios with description.
func tuningTalk(description string) pagestest.Video {
	return pagestest.Video{ID: videoA, Title: "Tuning SQLite", Author: "Example Talks", LengthSeconds: "754", Description: description}
}

// assertVideo checks a video written in fallback with its title, channel, content ("" for
// none) and reading time (0 for none).
func assertVideo(t *testing.T, h *harness, item store.Item, title, site, content string, minutes int64) {
	t.Helper()
	assertExtracted(t, item, "fallback")
	if item.Title != title || item.SiteName == nil || *item.SiteName != site {
		t.Errorf("title %q site %v, want %q and %q", item.Title, deref(item.SiteName), title, site)
	}
	got := h.content(item.ID)
	if (content == "") != (got == nil) || (got != nil && *got != content) {
		t.Errorf("content %v, want %q", deref(got), content)
	}
	if (minutes == 0) != (item.ReadingMinutes == nil) || (item.ReadingMinutes != nil && *item.ReadingMinutes != minutes) {
		t.Errorf("reading %v, want %d (0 for none)", deref(item.ReadingMinutes), minutes)
	}
	if item.ContentTruncated {
		t.Error("content_truncated = true, want false")
	}
}

// hang answers its headers and part of a body, then nothing until the request ends: the
// deadline of the download covers the body.
func hang(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", "text/html")
	_, _ = rw.Write([]byte("<html><head><title>Late</title></head><body>"))
	rw.(http.Flusher).Flush()
	<-r.Context().Done()
}

// timedHang is hang that tells, once the request ended, how long it lasted from its arrival:
// the share of the timeout the worker gave it.
func timedHang() (http.HandlerFunc, <-chan time.Duration) {
	lasted := make(chan time.Duration, 1)
	return func(rw http.ResponseWriter, r *http.Request) {
		begin := time.Now()
		hang(rw, r)
		lasted <- time.Since(begin)
	}, lasted
}

// assertLasted checks that a hanging request lasted between low and high shares of timeout.
func assertLasted(t *testing.T, name string, lasted <-chan time.Duration, timeout time.Duration, low, high float64) {
	t.Helper()
	select {
	case got := <-lasted:
		if got < time.Duration(low*float64(timeout)) || got > time.Duration(high*float64(timeout)) {
			t.Errorf("%s lasted %v, want between %.2f and %.2f of the %v timeout", name, got, low, high, timeout)
		}
	case <-time.After(waitLimit):
		t.Fatalf("%s never ended", name)
	}
}

func TestYouTubeExtractionWorker(t *testing.T) {
	// Requirement: A YouTube video is read with its metadata.

	t.Run("Video with a description", func(t *testing.T) {
		h := newHarness(t)
		description := multilineDescription()
		h.web.videoPage(videoA, tuningPage(tuningTalk(description)))
		h.web.oembedAnswer(videoA, http.StatusOK, "Wrong title", "Wrong channel")
		item := h.extractLink("https://www.youtube.com/watch?v=" + videoA + "&t=42s&utm_source=x")
		assertVideo(t, h, item, "Tuning SQLite", "Example Talks", description, 13)
		if got := h.web.requested(); !slices.Equal(got, []string{watchAsked}) {
			t.Errorf("requests %q, want the canonical page only", got)
		}
		if strings.Count(*h.content(item.ID), "\n") != 14 {
			t.Error("the line breaks of the description are lost")
		}
		if !strings.Contains(h.logs.String(), `"mode":"fallback","reason":"ok","format":"youtube","http_status":200`) {
			t.Errorf("logs do not tell the video read:\n%s", h.logs.String())
		}
	})

	t.Run("Short link, Short and live forms", func(t *testing.T) {
		h := newHarness(t)
		h.web.videoPage(videoA, tuningPage(tuningTalk("A talk.")))
		h.start()
		links := []string{
			"https://youtu.be/" + videoA + "?si=x", "https://m.youtube.com/shorts/" + videoA, "https://www.youtube.com/live/" + videoA,
		}
		for _, link := range links {
			captured := h.captureLink(link, false)
			h.waitPass()
			assertVideo(t, h, h.item(h.owner, captured.ID), "Tuning SQLite", "Example Talks", "A talk.", 13)
		}
		if got := h.web.requested(); !slices.Equal(got, []string{watchAsked, watchAsked, watchAsked}) {
			t.Errorf("requests %q, want the canonical page for each link", got)
		}
	})

	t.Run("Video without description", func(t *testing.T) {
		h := newHarness(t)
		video := tuningTalk("")
		video.LengthSeconds = "59"
		h.web.videoPage(videoA, tuningPage(video))
		assertVideo(t, h, h.extractLink(watchA), "Tuning SQLite", "Example Talks", "", 1)
	})

	t.Run("Live stream without duration", func(t *testing.T) {
		h := newHarness(t)
		video := tuningTalk("A live stream.")
		video.LengthSeconds = "0"
		h.web.videoPage(videoA, tuningPage(video))
		assertVideo(t, h, h.extractLink(watchA), "Tuning SQLite", "Example Talks", "A live stream.", 0)
	})

	t.Run("Very long description", func(t *testing.T) {
		h := newHarness(t)
		description, _ := pagestest.Words(130_000)
		h.web.videoPage(videoA, tuningPage(tuningTalk(description)))
		item := h.extractLink(watchA)
		assertExtracted(t, item, "fallback")
		content := h.content(item.ID)
		if content == nil || !item.ContentTruncated {
			t.Fatalf("content %v truncated %v, want the description cut", content != nil, item.ContentTruncated)
		}
		fields := strings.Fields(*content)
		if n := len([]rune(*content)); n > extraction.MaxContentLength || !pagestest.IsWord(fields[len(fields)-1]) {
			t.Errorf("content of %d code points, want at most %d ending on a whole word", n, extraction.MaxContentLength)
		}
		if item.ReadingMinutes == nil || *item.ReadingMinutes != 13 {
			t.Errorf("reading %v, want the duration, 13", deref(item.ReadingMinutes))
		}
	})

	t.Run("Player data of another video are ignored", func(t *testing.T) {
		h := newHarness(t)
		other := pagestest.Video{ID: videoB, Title: "Other video", Author: "Other channel", LengthSeconds: "60", Description: "Other."}
		h.web.videoPage(videoA, tuningPage(other))
		h.web.oembedAnswer(videoA, http.StatusOK, "Tuning SQLite", "Example Talks")
		assertVideo(t, h, h.extractLink(watchA), "Tuning SQLite", "Example Talks", "", 0)
	})

	t.Run("Private video links are extracted too", func(t *testing.T) {
		h := newHarness(t)
		h.web.videoPage(videoA, tuningPage(tuningTalk("A talk.")))
		h.start()
		captured := h.captureLink(watchA, true)
		h.waitPass()
		item := h.item(h.owner, captured.ID)
		assertVideo(t, h, item, "Tuning SQLite", "Example Talks", "A talk.", 13)
		if !item.IsPrivate {
			t.Error("the item is no longer private")
		}
		if got := h.web.requested(); !slices.Equal(got, []string{watchAsked}) {
			t.Errorf("requests %q, want the canonical page, the video id only (RG-92)", got)
		}
	})

	// Requirement: A video whose page gives no metadata falls back on oEmbed.

	t.Run("Page without player data", func(t *testing.T) {
		h := newHarness(t)
		h.web.videoPage(videoA, pagestest.VideoPage("Tuning SQLite", "var ytInitialData = {};"))
		h.web.oembedAnswer(videoA, http.StatusOK, "Tuning SQLite", "Example Talks")
		assertVideo(t, h, h.extractLink(watchA), "Tuning SQLite", "Example Talks", "", 0)
		if got := h.web.requested(); !slices.Equal(got, []string{watchAsked, oembedAsked}) {
			t.Errorf("requests %q, want the canonical page then its oEmbed", got)
		}
		headers := h.web.headersOf("https://www.youtube.com/oembed")
		if headers.Get("Accept") != "application/json" || headers.Get("User-Agent") != "Hippo/test (personal read-later)" || headers.Get("Cookie") != "" {
			t.Errorf("oEmbed headers = %v", headers)
		}
		if got := h.web.headersOf(watchA).Get("Accept"); got != "text/html,application/xhtml+xml,application/pdf;q=0.9" {
			t.Errorf("page Accept = %q", got)
		}
		if !strings.Contains(h.logs.String(), `"reason":"oembed","format":"youtube","http_status":200,"oembed_status":200`) {
			t.Errorf("logs do not tell the oEmbed fallback:\n%s", h.logs.String())
		}
	})

	t.Run("Unavailable video", func(t *testing.T) {
		h := newHarness(t)
		h.web.videoPage(videoA, pagestest.VideoPage("YouTube", pagestest.PlayerScript(pagestest.UnavailablePlayerResponse())))
		h.web.oembedAnswer(videoA, http.StatusBadRequest, "", "")
		assertVideo(t, h, h.extractLink(watchA), "www.youtube.com", "www.youtube.com", "", 0)
		if !strings.Contains(h.logs.String(), `"reason":"no_metadata","format":"youtube","http_status":200,"oembed_status":400`) {
			t.Errorf("logs do not tell the missing metadata:\n%s", h.logs.String())
		}
	})

	t.Run("Consent page", func(t *testing.T) {
		h := newHarness(t)
		const consent = "https://consent.youtube.com/m"
		h.web.video(videoA, func(rw http.ResponseWriter, r *http.Request) {
			http.SetCookie(rw, &http.Cookie{Name: "CONSENT", Value: "PENDING+1", Domain: "youtube.com"})
			http.Redirect(rw, r, consent+"?continue=x", http.StatusSeeOther)
		})
		h.web.page(consent, "text/html", pagestest.MetaOnly("Before you continue to YouTube", "Cookies and data"))
		h.web.oembedAnswer(videoA, http.StatusOK, "Tuning SQLite", "Example Talks")
		assertVideo(t, h, h.extractLink(watchA), "Tuning SQLite", "Example Talks", "", 0)
		if cookie := h.web.headersOf("https://www.youtube.com/oembed").Get("Cookie"); cookie != "" {
			t.Errorf("oEmbed sent the cookie %q of the page", cookie)
		}
	})

	t.Run("Video page that does not answer in time", func(t *testing.T) {
		h := newHarness(t)
		h.timeout = 900 * time.Millisecond // the 15 s of production, shortened: 600 ms for the page, 300 for oEmbed
		page, pageLasted := timedHang()
		h.web.video(videoA, page)
		h.web.oembedAnswer(videoA, http.StatusOK, "Tuning SQLite", "Example Talks")
		begin := time.Now()
		item := h.extractLink(watchA)
		elapsed := time.Since(begin)
		assertVideo(t, h, item, "Tuning SQLite", "Example Talks", "", 0)
		// The page has 2/3 of the timeout: cut before the whole of it, but after more than 1/3.
		assertLasted(t, "the page", pageLasted, h.timeout, 0.4, 0.8)
		if elapsed > h.timeout*3/2 {
			t.Errorf("extracted after %v, want within the %v timeout", elapsed, h.timeout)
		}
		if !strings.Contains(h.logs.String(), `"reason":"oembed"`) {
			t.Errorf("logs do not tell the oEmbed fallback:\n%s", h.logs.String())
		}
	})

	t.Run("Neither the page nor oEmbed answers in time", func(t *testing.T) {
		h := newHarness(t)
		h.timeout = 900 * time.Millisecond // the 15 s of production, shortened: 600 ms for the page, 300 for oEmbed
		page, pageLasted := timedHang()
		oembed, oembedLasted := timedHang()
		h.web.video(videoA, page)
		h.web.oembed(videoA, oembed)
		begin := time.Now()
		item := h.extractLink("https://youtu.be/" + videoA)
		elapsed := time.Since(begin)
		assertVideo(t, h, item, "youtu.be", "youtu.be", "", 0)
		// 2/3 of the timeout for the page, 1/3 for oEmbed: about the timeout in total (RG-23).
		assertLasted(t, "the page", pageLasted, h.timeout, 0.4, 0.8)
		assertLasted(t, "oEmbed", oembedLasted, h.timeout, 0.05, 0.5)
		if elapsed > h.timeout*3/2 {
			t.Errorf("extracted after %v, want within the %v timeout", elapsed, h.timeout)
		}
		if got := h.web.requested(); !slices.Equal(got, []string{watchAsked, oembedAsked}) {
			t.Errorf("requests %q, want the canonical page then its oEmbed", got)
		}
		if !strings.Contains(h.logs.String(), `"mode":"fallback","reason":"timeout","format":"youtube"`) {
			t.Errorf("logs do not tell the timeout:\n%s", h.logs.String())
		}
	})

	t.Run("A NAS network outage leaves a video queued", func(t *testing.T) {
		h := newHarness(t)
		h.web.videoPage(videoA, tuningPage(tuningTalk("A talk.")))
		h.web.oembedAnswer(videoA, http.StatusOK, "Tuning SQLite", "Example Talks")
		h.web.failDNS.Store(true)
		h.start()
		captured := h.captureLink(watchA, false)
		h.waitPass()
		if item := h.item(h.owner, captured.ID); item.Status != "queued" || item.ExtractionMode != nil {
			t.Fatalf("during the outage: status %q mode %q, want queued", item.Status, mode(item))
		}
		if queued := h.queued(h.owner); len(queued) != 1 || queued[0].AttemptStarted {
			t.Fatalf("queued = %+v, want the item without an unfinished attempt", queued)
		}
		if lookups := h.web.lookups.Load(); lookups != 1 {
			t.Errorf("%d host lookups, want 1: oEmbed is not asked after an outage", lookups)
		}

		h.web.failDNS.Store(false)
		h.endPause()
		h.waitPass()
		assertVideo(t, h, h.item(h.owner, captured.ID), "Tuning SQLite", "Example Talks", "A talk.", 13)
	})

	t.Run("A video that crashed the backend is not fetched again", func(t *testing.T) {
		h := newHarness(t)
		h.web.videoPage(videoA, tuningPage(tuningTalk("A talk.")))
		h.web.oembedAnswer(videoA, http.StatusOK, "Tuning SQLite", "Example Talks")
		captured := h.captureLink(watchA, false)
		if err := h.store.StartExtractionAttempt(t.Context(), h.owner, captured.ID); err != nil {
			t.Fatalf("StartExtractionAttempt: %v", err)
		}
		h.start()
		assertVideo(t, h, h.item(h.owner, captured.ID), "www.youtube.com", "www.youtube.com", "", 0)
		if got, lookups := h.web.requested(), h.web.lookups.Load(); len(got)+int(lookups) != 0 {
			t.Errorf("requests %q, %d lookups; want none", got, lookups)
		}
		if !strings.Contains(h.logs.String(), `"reason":"crashed_before"`) {
			t.Errorf("logs do not tell the earlier crash:\n%s", h.logs.String())
		}
	})

	// Requirement: A YouTube link that is not a video is read as a web page.

	t.Run("Channel page", func(t *testing.T) {
		h := newHarness(t)
		const channel = "https://www.youtube.com/@exampletalks"
		h.web.page(channel, "text/html", pagestest.MetaOnly("Example Talks", "Talks about databases."))
		item := h.extractLink(channel)
		assertExtracted(t, item, "fallback")
		if item.Title != "Example Talks" {
			t.Errorf("title = %q, want Example Talks", item.Title)
		}
		if got := h.web.requested(); !slices.Equal(got, []string{"www.youtube.com/@exampletalks?"}) {
			t.Errorf("requests %q, want the channel page itself", got)
		}
	})

	// Requirement: Links left waiting as YouTube links are picked up.

	t.Run("Waiting YouTube link", func(t *testing.T) {
		h := newHarness(t)
		const channel = "https://www.youtube.com/@exampletalks"
		h.web.videoPage(videoA, tuningPage(tuningTalk("A talk.")))
		h.web.page(channel, "text/html", pagestest.MetaOnly("Example Talks", "Talks about databases."))
		// Captured while E2-05 ran: left queued, with no attempt row nor retry counted.
		video := h.captureLink("https://youtu.be/"+videoA, false)
		channelItem := h.captureLink(channel, false)
		h.start()
		assertVideo(t, h, h.item(h.owner, video.ID), "Tuning SQLite", "Example Talks", "A talk.", 13)
		if item := h.item(h.owner, channelItem.ID); item.Status != "extracted" || item.Title != "Example Talks" {
			t.Errorf("channel: status %q title %q, want extracted as a page", item.Status, item.Title)
		}
	})

	// Requirement: Video extraction keeps content out of the logs.

	t.Run("Video extraction logs no content", func(t *testing.T) {
		h := newHarness(t)
		secret := pagestest.Video{
			ID: videoA, Title: "Private thought", Author: "Secret Channel", LengthSeconds: "754",
			Description: strings.Repeat("a private sentence ", 40),
		}
		h.web.videoPage(videoA, tuningPage(secret))
		h.web.video(videoB, func(rw http.ResponseWriter, _ *http.Request) {
			http.Error(rw, "Internal Server Error", http.StatusInternalServerError)
		})
		h.web.oembedAnswer(videoB, http.StatusOK, "Private thought", "Secret Channel")
		h.start()
		first := h.captureLink(watchA, false)
		h.waitPass()
		second := h.captureLink("https://www.youtube.com/watch?v="+videoB, false)
		h.waitPass()
		for _, id := range []string{first.ID, second.ID} {
			if item := h.item(h.owner, id); item.Title != "Private thought" {
				t.Fatalf("title = %q, want the video title", item.Title)
			}
		}
		logs := h.logs.String()
		for _, secret := range []string{videoA, videoB, "youtube.com", "youtu.be", "Private thought", "Secret Channel", "private sentence"} {
			if strings.Contains(logs, secret) {
				t.Errorf("logs contain %q", secret)
			}
		}
		for _, want := range []string{first.ID, second.ID, `"reason":"ok"`, `"reason":"oembed"`, `"http_status":500`, `"oembed_status":200`, `"duration_ms"`} {
			if !strings.Contains(logs, want) {
				t.Errorf("logs lack %s:\n%s", want, logs)
			}
		}
	})

	// Beyond the scenarios: the checks of design.md.

	t.Run("a page redirected off YouTube gives no metadata", func(t *testing.T) {
		h := newHarness(t)
		const sorry = "https://sorry.example.com/sorry/index"
		h.web.video(videoA, func(rw http.ResponseWriter, r *http.Request) { http.Redirect(rw, r, sorry, http.StatusFound) })
		h.web.page(sorry, "text/html", tuningPage(tuningTalk("A talk."))) // player data, but not YouTube's
		h.web.oembedAnswer(videoA, http.StatusOK, "Tuning SQLite", "Example Talks")
		assertVideo(t, h, h.extractLink(watchA), "Tuning SQLite", "Example Talks", "", 0)
	})

	t.Run("an oEmbed answer redirected off YouTube is not read", func(t *testing.T) {
		h := newHarness(t)
		const elsewhere = "https://oembed.example.com/answer"
		h.web.videoPage(videoA, pagestest.VideoPage("x"))
		h.web.oembed(videoA, func(rw http.ResponseWriter, r *http.Request) { http.Redirect(rw, r, elsewhere, http.StatusFound) })
		h.web.page(elsewhere, "application/json", oembedJSON(videoA, "Tuning SQLite", "Example Talks"))
		assertVideo(t, h, h.extractLink(watchA), "www.youtube.com", "www.youtube.com", "", 0)
	})

	t.Run("an oEmbed answer over 64 KiB is not read", func(t *testing.T) {
		h := newHarness(t)
		h.web.videoPage(videoA, pagestest.VideoPage("x"))
		h.web.oembedAnswer(videoA, http.StatusOK, "Tuning SQLite", strings.Repeat("x", extraction.MaxJSONSize))
		assertVideo(t, h, h.extractLink(watchA), "www.youtube.com", "www.youtube.com", "", 0)
	})

	t.Run("a page over 2 MB falls back on oEmbed", func(t *testing.T) {
		h := newHarness(t)
		description, _ := pagestest.Words(extraction.MaxBodySize / 2)
		h.web.videoPage(videoA, tuningPage(tuningTalk(description)))
		h.web.oembedAnswer(videoA, http.StatusOK, "Tuning SQLite", "Example Talks")
		assertVideo(t, h, h.extractLink(watchA), "Tuning SQLite", "Example Talks", "", 0)
	})

	t.Run("a network outage on oEmbed leaves the video queued", func(t *testing.T) {
		h := newHarness(t)
		h.web.video(videoA, func(rw http.ResponseWriter, _ *http.Request) {
			h.web.failDNS.Store(true) // the network of the NAS goes down after the page
			rw.Header().Set("Content-Type", "text/html")
			_, _ = rw.Write(pagestest.VideoPage("x"))
		})
		h.web.oembedAnswer(videoA, http.StatusOK, "Tuning SQLite", "Example Talks")
		h.start()
		captured := h.captureLink(watchA, false)
		h.waitPass()
		if item := h.item(h.owner, captured.ID); item.Status != "queued" {
			t.Fatalf("status %q, want queued", item.Status)
		}
		if queued := h.queued(h.owner); len(queued) != 1 || queued[0].AttemptStarted {
			t.Fatalf("queued = %+v, want the item without an unfinished attempt", queued)
		}
		if !strings.Contains(h.logs.String(), `"reason":"network"`) {
			t.Errorf("logs do not tell the outage:\n%s", h.logs.String())
		}
	})

	t.Run("a stop during oEmbed leaves the video queued", func(t *testing.T) {
		h := newHarness(t)
		arrived := make(chan struct{}, 1)
		h.web.videoPage(videoA, pagestest.VideoPage("x"))
		h.web.oembed(videoA, func(rw http.ResponseWriter, r *http.Request) {
			arrived <- struct{}{}
			hang(rw, r)
		})
		stop := h.start()
		captured := h.captureLink(watchA, false)
		select {
		case <-arrived:
		case <-time.After(waitLimit):
			t.Fatal("oEmbed was never asked")
		}
		stop()
		if item := h.item(h.owner, captured.ID); item.Status != "queued" || item.ExtractionMode != nil {
			t.Fatalf("after the stop: status %q mode %q, want queued", item.Status, mode(item))
		}
		if queued := h.queued(h.owner); len(queued) != 1 || queued[0].AttemptStarted {
			t.Fatalf("queued = %+v, want the item without an unfinished attempt", queued)
		}
	})

	t.Run("a page reader that cannot start leaves the video queued", func(t *testing.T) {
		h := newHarness(t)
		h.reader = []string{filepath.Join(t.TempDir(), "missing-reader"), extraction.ChildCommand}
		h.web.videoPage(videoA, tuningPage(tuningTalk("A talk.")))
		h.web.oembedAnswer(videoA, http.StatusOK, "Tuning SQLite", "Example Talks")
		h.start()
		captured := h.captureLink(watchA, false)
		h.waitPass()
		if item := h.item(h.owner, captured.ID); item.Status != "queued" {
			t.Fatalf("status %q, want queued", item.Status)
		}
		if queued := h.queued(h.owner); len(queued) != 1 || queued[0].AttemptStarted {
			t.Errorf("queued = %+v, want the item without an unfinished attempt", queued)
		}
		if got := h.web.requested(); !slices.Equal(got, []string{watchAsked}) {
			t.Errorf("requests %q, want the page only: oEmbed waits for the reader", got)
		}
		if !strings.Contains(h.logs.String(), `"reason":"reader_unavailable"`) {
			t.Errorf("logs do not tell the missing reader:\n%s", h.logs.String())
		}
	})

	t.Run("every form of a video counts as one host after an outage", func(t *testing.T) {
		h := newHarness(t)
		h.web.failHost("www.youtube.com")
		h.web.page("https://blog.example.com/post", "text/html", fixture(t, "article.html"))
		h.start()
		h.relay.mute(true)
		first := h.captureLink(watchA, false)
		second := h.captureLink("https://youtu.be/"+videoB, false)
		post := h.captureLink("https://blog.example.com/post", false)
		h.relay.wakeNow()
		h.waitPass()
		for _, id := range []string{first.ID, second.ID} {
			if item := h.item(h.owner, id); item.Status != "queued" {
				t.Errorf("video %s: status %q, want queued", id, item.Status)
			}
		}
		assertExtracted(t, h.item(h.owner, post.ID), "full")
		if lookups := h.web.lookups.Load(); lookups != 2 {
			t.Errorf("%d host lookups, want 2: the first video and the other host, not the second video", lookups)
		}
	})
}
