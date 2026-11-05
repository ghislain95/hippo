package extraction_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ghislain95/hippo/backend/internal/extraction"
	"github.com/ghislain95/hippo/backend/internal/extraction/pagestest"
)

func TestVideoID(t *testing.T) {
	const id = "aaaaaaaaaa1"
	cases := []struct {
		name, url string
		want      string // "" when the URL names no video
	}{
		// Each form of a video URL.
		{"watch", "https://www.youtube.com/watch?v=" + id, id},
		{"watch with other parameters", "https://www.youtube.com/watch?t=42s&v=" + id + "&utm_source=x", id},
		{"watch in a playlist", "https://www.youtube.com/watch?v=" + id + "&list=PLxyz", id},
		{"watch with an escaped id", "https://www.youtube.com/watch?v=aaaaaaaaa%2D1", "aaaaaaaaa-1"},
		{"watch without www", "https://youtube.com/watch?v=" + id, id},
		{"watch on the mobile site", "https://m.youtube.com/watch?v=" + id, id},
		{"watch on music", "https://music.youtube.com/watch?v=" + id, id},
		{"watch over http with a port", "http://www.youtube.com:8080/watch?v=" + id, id},
		{"host in capitals with a final dot", "https://WWW.YouTube.COM./watch?v=" + id, id},
		{"short link", "https://youtu.be/" + id, id},
		{"short link with tracking", "https://youtu.be/" + id + "?si=abc&t=10", id},
		{"short link with a trailing slash", "https://youtu.be/" + id + "/", id},
		{"short", "https://www.youtube.com/shorts/" + id, id},
		{"short on the mobile site", "https://m.youtube.com/shorts/" + id, id},
		{"short with a trailing slash", "https://www.youtube.com/shorts/" + id + "/", id},
		{"live", "https://www.youtube.com/live/" + id + "?feature=share", id},
		{"embed", "https://www.youtube.com/embed/" + id, id},
		{"embed with further segments", "https://www.youtube.com/embed/" + id + "/more", id},
		{"old form", "https://www.youtube.com/v/" + id, id},
		{"id with a dash and an underscore", "https://youtu.be/a-b_c-d_e-f", "a-b_c-d_e-f"},
		{"two v, the first one counts", "https://www.youtube.com/watch?v=" + id + "&v=bbbbbbbbbb2", id},

		// Ids of the wrong length or with other characters.
		{"id of 10 characters", "https://www.youtube.com/watch?v=aaaaaaaaaa", ""},
		{"id of 12 characters", "https://www.youtube.com/watch?v=aaaaaaaaaa12", ""},
		{"short link id of 10 characters", "https://youtu.be/aaaaaaaaaa", ""},
		{"short id of 12 characters", "https://www.youtube.com/shorts/aaaaaaaaaa12", ""},
		{"id with a dot", "https://www.youtube.com/watch?v=aaaaaaaaa.1", ""},
		{"two v, the first one invalid", "https://www.youtube.com/watch?v=bad&v=" + id, ""},
		{"watch without v", "https://www.youtube.com/watch?list=PLxyz", ""},
		{"watch with an empty v", "https://www.youtube.com/watch?v=", ""},
		{"shorts without id", "https://www.youtube.com/shorts/", ""},

		// YouTube links that are not videos.
		{"home page", "https://www.youtube.com/", ""},
		{"channel handle", "https://www.youtube.com/@exampletalks", ""},
		{"channel id", "https://www.youtube.com/channel/UCxxxxxxxxxxxxxxxxxxxxxx", ""},
		{"playlist", "https://www.youtube.com/playlist?list=PLxyz", ""},
		{"search", "https://www.youtube.com/results?search_query=sqlite", ""},
		{"id as a first segment on youtube.com", "https://www.youtube.com/" + id, ""},
		{"short link home", "https://youtu.be/", ""},

		// Hosts that only look like YouTube.
		{"youtube-nocookie", "https://www.youtube-nocookie.com/embed/" + id, ""},
		{"youtube.com under another domain", "https://youtube.com.example.com/watch?v=" + id, ""},
		{"a subdomain of youtu.be", "https://www.youtu.be/" + id, ""},
		{"a domain ending like youtube.com", "https://notyoutube.com/watch?v=" + id, ""},
		{"user info naming YouTube", "https://www.youtube.com@example.com/watch?v=" + id, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := extraction.VideoID(mustURL(t, tc.url))
			if ok != (tc.want != "") || got != tc.want {
				t.Errorf("videoID(%s) = %q, %v; want %q", tc.url, got, ok, tc.want)
			}
		})
	}

	t.Run("the canonical page and oEmbed carry the id only", func(t *testing.T) {
		if got := extraction.CanonicalPage(id).String(); got != "https://www.youtube.com/watch?v="+id {
			t.Errorf("canonical page = %s", got)
		}
		want := "https://www.youtube.com/oembed?url=https%3A%2F%2Fwww.youtube.com%2Fwatch%3Fv%3D" + id + "&format=json"
		if got := extraction.OEmbedURL(id).String(); got != want {
			t.Errorf("oEmbed URL = %s, want %s", got, want)
		}
	})
}

// tuningVideo is the fictional video of the scenarios.
func tuningVideo() pagestest.Video {
	return pagestest.Video{
		ID: "aaaaaaaaaa1", Title: "Tuning SQLite", Author: "Example Talks", LengthSeconds: "754",
		Description: "A talk about SQLite pragmas.\n\nChapters:\n0:00 Intro\n1:30 WAL mode\n\nNotes: https://example.com/notes",
	}
}

// videoPage is a watch page whose player data hold details.
func videoPage(details map[string]any) []byte {
	return pagestest.VideoPage("Tuning SQLite", pagestest.PlayerScript(pagestest.PlayerResponse(details, 3)))
}

// detailsWith is the details of the fictional video with field set to value, or removed when
// value is nil.
func detailsWith(field string, value any) map[string]any {
	d := pagestest.Details(tuningVideo())
	if value == nil {
		delete(d, field)
	} else {
		d[field] = value
	}
	return d
}

func TestReadYouTube(t *testing.T) {
	const id, provisional = "aaaaaaaaaa1", "www.youtube.com"
	nested := strings.Repeat(`{"a":`, 10_001) + "1" + strings.Repeat("}", 10_001)
	valid := string(pagestest.PlayerResponse(pagestest.Details(tuningVideo()), 3))
	other := pagestest.Details(pagestest.Video{ID: "bbbbbbbbbb2", Title: "Other video", Author: "Other channel", LengthSeconds: "60"})

	type want struct {
		reason, title, site, content string // content "" means none
		minutes                      int64  // 0 means none
	}
	read := want{reason: "ok", title: "Tuning SQLite", site: "Example Talks", content: tuningVideo().Description, minutes: 13}
	none := want{reason: "no_metadata", title: provisional, site: provisional}
	with := func(f func(*want)) want {
		w := read
		f(&w)
		return w
	}
	cases := []struct {
		name string
		page []byte
		want want
	}{
		{"fields read", videoPage(pagestest.Details(tuningVideo())), read},
		{"token absent", pagestest.VideoPage("Tuning SQLite", "var ytInitialData = {};"), none},
		{"= null before the real block", pagestest.VideoPage("Tuning SQLite", "var ytInitialPlayerResponse = null;", pagestest.PlayerScript([]byte(valid))), read},
		{"token without assignment before the real block", pagestest.VideoPage("Tuning SQLite", "if (window.ytInitialPlayerResponse) {}", pagestest.PlayerScript([]byte(valid))), read},
		{"three occurrences without data before the real block", pagestest.VideoPage("Tuning SQLite",
			"var ytInitialPlayerResponse = null;", "ytInitialPlayerResponse = null;", "ytInitialPlayerResponse = null;", pagestest.PlayerScript([]byte(valid))), none},
		{"malformed JSON", pagestest.VideoPage("Tuning SQLite", "var ytInitialPlayerResponse = "+valid[:len(valid)/2]), none},
		{"10 001 levels of nesting", pagestest.VideoPage("Tuning SQLite", "var ytInitialPlayerResponse = "+valid[:len(valid)-1]+`,"deep":`+nested+"};"), none},
		{"player data without videoDetails", pagestest.VideoPage("Tuning SQLite", pagestest.PlayerScript(pagestest.UnavailablePlayerResponse())), none},
		{"videoDetails of another type", pagestest.VideoPage("Tuning SQLite", `var ytInitialPlayerResponse = {"videoDetails":"aaaaaaaaaa1"};`), none},
		{"another id only", videoPage(other), none},
		{"another id then the right one", pagestest.VideoPage("Tuning SQLite",
			pagestest.PlayerScript(pagestest.PlayerResponse(other, 3)), pagestest.PlayerScript([]byte(valid))), read},
		{"empty title", videoPage(detailsWith("title", " \n ")), none},
		{"title of another type", videoPage(detailsWith("title", 42)), none},
		{"title absent", videoPage(detailsWith("title", nil)), none},
		{
			"title in invalid UTF-8", pagestest.VideoPage("Tuning SQLite", `var ytInitialPlayerResponse = {"videoDetails":{"videoId":"aaaaaaaaaa1","title":"Tun`+"\xff"+`ing  SQLite"}};`),
			want{reason: "ok", title: "Tun\uFFFDing SQLite", site: provisional},
		},
		{"author of another type", videoPage(detailsWith("author", map[string]any{"name": "x"})), with(func(w *want) { w.site = provisional })},
		{"empty author", videoPage(detailsWith("author", "")), with(func(w *want) { w.site = provisional })},
		{"lengthSeconds abc", videoPage(detailsWith("lengthSeconds", "abc")), with(func(w *want) { w.minutes = 0 })},
		{"lengthSeconds -5", videoPage(detailsWith("lengthSeconds", "-5")), with(func(w *want) { w.minutes = 0 })},
		{"lengthSeconds 0", videoPage(detailsWith("lengthSeconds", "0")), with(func(w *want) { w.minutes = 0 })},
		{"lengthSeconds 754 as a number", videoPage(detailsWith("lengthSeconds", 754)), read},
		{"lengthSeconds 754.5", videoPage(detailsWith("lengthSeconds", "754.5")), with(func(w *want) { w.minutes = 0 })},
		{"lengthSeconds 9999999", videoPage(detailsWith("lengthSeconds", "9999999")), with(func(w *want) { w.minutes = 0 })},
		{"lengthSeconds 1000000", videoPage(detailsWith("lengthSeconds", "1000000")), with(func(w *want) { w.minutes = 16_667 })},
		{"lengthSeconds 59", videoPage(detailsWith("lengthSeconds", "59")), with(func(w *want) { w.minutes = 1 })},
		{"lengthSeconds 61", videoPage(detailsWith("lengthSeconds", "61")), with(func(w *want) { w.minutes = 2 })},
		{"lengthSeconds absent", videoPage(detailsWith("lengthSeconds", nil)), with(func(w *want) { w.minutes = 0 })},
		{"lengthSeconds of another type", videoPage(detailsWith("lengthSeconds", true)), with(func(w *want) { w.minutes = 0 })},
		{
			"description with carriage returns, NUL and tabs", videoPage(detailsWith("shortDescription", " Line one\r\nLine\x00 two\tend\r\n\u0085 ")),
			with(func(w *want) { w.content = "Line one\nLine two\tend" }),
		},
		{"empty description", videoPage(detailsWith("shortDescription", " \n ")), with(func(w *want) { w.content = "" })},
		{"description of another type", videoPage(detailsWith("shortDescription", []string{"x"})), with(func(w *want) { w.content = "" })},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extraction.ReadYouTube(tc.page, id, provisional)
			assertVideoOutcome(t, got, tc.want.reason, tc.want.title, tc.want.site, tc.want.content, tc.want.minutes)
		})
	}

	t.Run("description of 130 000 characters", func(t *testing.T) {
		description, _ := pagestest.Words(130_000)
		got := extraction.ReadYouTube(videoPage(detailsWith("shortDescription", description)), id, provisional)
		content := got.Extraction.ContentText
		if content == nil || !got.Extraction.ContentTruncated {
			t.Fatalf("content %v truncated %v, want the description cut", content != nil, got.Extraction.ContentTruncated)
		}
		fields := strings.Fields(*content)
		if n := len([]rune(*content)); n > extraction.MaxContentLength || !pagestest.IsWord(fields[len(fields)-1]) {
			t.Errorf("content of %d code points, want at most %d ending on a whole word", n, extraction.MaxContentLength)
		}
		if got.Extraction.ReadingMinutes == nil || *got.Extraction.ReadingMinutes != 13 {
			t.Errorf("reading %v, want the duration, 13", got.Extraction.ReadingMinutes)
		}
	})

	t.Run("an empty id matches no player data", func(t *testing.T) {
		got := extraction.ReadYouTube(videoPage(detailsWith("videoId", "")), "", provisional)
		assertVideoOutcome(t, got, "no_metadata", provisional, provisional, "", 0)
	})

	t.Run("a title of 2 000 characters is bounded", func(t *testing.T) {
		title, _ := pagestest.Words(2_000)
		got := extraction.ReadYouTube(videoPage(detailsWith("title", title)), id, provisional)
		if got.Extraction.Title == nil || len([]rune(*got.Extraction.Title)) > extraction.MaxTitleLength {
			t.Errorf("title %v, want at most %d code points", got.Extraction.Title, extraction.MaxTitleLength)
		}
	})
}

// assertVideoOutcome checks the outcome of a video: always fallback, never truncated except a
// long description; content "" and minutes 0 mean none.
func assertVideoOutcome(t *testing.T, got extraction.Outcome, reason, title, site, content string, minutes int64) {
	t.Helper()
	e := got.Extraction
	if got.Reason != reason || e.Mode != "fallback" {
		t.Errorf("reason %q mode %q, want %q, fallback", got.Reason, e.Mode, reason)
	}
	if e.Title == nil || *e.Title != title || e.SiteName == nil || *e.SiteName != site {
		t.Errorf("title %v site %v, want %q and %q", deref(e.Title), deref(e.SiteName), title, site)
	}
	if (content == "") != (e.ContentText == nil) || (e.ContentText != nil && *e.ContentText != content) || e.ContentTruncated {
		t.Errorf("content %q truncated %v, want %q", deref(e.ContentText), e.ContentTruncated, content)
	}
	if (minutes == 0) != (e.ReadingMinutes == nil) || (e.ReadingMinutes != nil && *e.ReadingMinutes != minutes) {
		t.Errorf("reading %v, want %d (0 for none)", e.ReadingMinutes, minutes)
	}
}

func TestReadYouTubeInChild(t *testing.T) {
	ctx := context.Background()
	const id, provisional = "aaaaaaaaaa1", "www.youtube.com"
	page := videoPage(pagestest.Details(tuningVideo()))

	t.Run("the player data are read in the child", func(t *testing.T) {
		got, err := extraction.ReadVideoInChild(ctx, childArgv(extraction.ChildCommand), extraction.ReadTimeout, page, id, provisional)
		if err != nil {
			t.Fatal(err)
		}
		assertVideoOutcome(t, got, "ok", "Tuning SQLite", "Example Talks", tuningVideo().Description, 13)
		if got.Format != "youtube" {
			t.Errorf("format %q, want youtube", got.Format)
		}
	})

	t.Run("a page without player data answers no_metadata", func(t *testing.T) {
		got, err := extraction.ReadVideoInChild(ctx, childArgv(extraction.ChildCommand), extraction.ReadTimeout, pagestest.VideoPage("x"), id, provisional)
		if err != nil {
			t.Fatal(err)
		}
		assertVideoOutcome(t, got, "no_metadata", provisional, provisional, "", 0)
	})

	t.Run("a description of 2 MiB of quotes stays under the output bound", func(t *testing.T) {
		head := videoPage(detailsWith("shortDescription", ""))
		// Each quote is two bytes of JSON, and the description is twice in the player data.
		quotes := (extraction.MaxBodySize - len(head) - 1024) / 4
		body := videoPage(detailsWith("shortDescription", strings.Repeat(`"`, quotes)))
		if len(body) > extraction.MaxBodySize || len(body) < extraction.MaxBodySize-4096 {
			t.Fatalf("page of %d bytes, want about 2 MiB", len(body))
		}
		got, err := extraction.ReadVideoInChild(ctx, childArgv(extraction.ChildCommand), extraction.ReadTimeout, body, id, provisional)
		if err != nil || got.Reason != "ok" || got.Extraction.ContentText == nil {
			t.Fatalf("outcome %q, %v; want ok with content", got.Reason, err)
		}
		if *got.Extraction.ContentText != strings.Repeat(`"`, extraction.MaxContentLength) || !got.Extraction.ContentTruncated {
			t.Errorf("content of %d bytes, truncated %v; want %d quotes, truncated", len(*got.Extraction.ContentText), got.Extraction.ContentTruncated, extraction.MaxContentLength)
		}
	})

	t.Run("a child killed at the time limit falls back on timeout", func(t *testing.T) {
		fifo, _ := startedFIFO(t)
		begin := time.Now()
		got, err := extraction.ReadVideoInChild(ctx, childArgv(hangChild, fifo), 300*time.Millisecond, page, id, provisional)
		if err != nil || got.Reason != "timeout" || got.Extraction.Mode != "fallback" {
			t.Errorf("outcome %q, %q, %v; want fallback, timeout", got.Extraction.Mode, got.Reason, err)
		}
		if elapsed := time.Since(begin); elapsed > 5*time.Second {
			t.Errorf("answer after %v, want about the time limit", elapsed)
		}
	})

	t.Run("a child over its memory limit falls back on too_large", func(t *testing.T) {
		got, err := extraction.ReadVideoInChild(ctx, childArgv(memoryChild), extraction.ReadTimeout, page, id, provisional)
		if err != nil || got.Reason != "too_large" {
			t.Errorf("outcome %q, %v; want too_large", got.Reason, err)
		}
	})

	t.Run("a child that answers too much falls back on reading_failed", func(t *testing.T) {
		got, err := extraction.ReadVideoInChild(ctx, childArgv(floodChild), extraction.ReadTimeout, page, id, provisional)
		if err != nil || got.Reason != "reading_failed" {
			t.Errorf("outcome %q, %v; want reading_failed", got.Reason, err)
		}
	})

	for _, mode := range []string{pdfReasonChild, fullChild, crashChild} {
		t.Run("an answer that is not a video's falls back: "+mode, func(t *testing.T) {
			got, err := extraction.ReadVideoInChild(ctx, childArgv(mode), extraction.ReadTimeout, page, id, provisional)
			if err != nil || got.Reason != "reading_failed" || got.Extraction.Mode != "fallback" {
				t.Errorf("outcome %q, %q, %v; want fallback, reading_failed", got.Extraction.Mode, got.Reason, err)
			}
			assertVideoOutcome(t, got, "reading_failed", provisional, provisional, "", 0)
		})
	}

	t.Run("a stop of the worker during the reading leaves the item to retry", func(t *testing.T) {
		fifo, started := startedFIFO(t)
		stopCtx, stop := context.WithCancel(ctx)
		go func() {
			<-started
			stop()
		}()
		_, err := extraction.ReadVideoInChild(stopCtx, childArgv(hangChild, fifo), extraction.ReadTimeout, page, id, provisional)
		if !errors.Is(err, extraction.ErrReaderStopped) {
			t.Errorf("error = %v, want the reading stopped", err)
		}
	})
}
