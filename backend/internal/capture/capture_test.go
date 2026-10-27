package capture_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ghislain95/hippo/backend/internal/capture"
	"github.com/ghislain95/hippo/backend/internal/store"
	"github.com/ghislain95/hippo/backend/internal/store/storetest"
)

const testClientID = "01JB8Z6Q4M1X2Y3Z4A5B6C7D8E"

func ptr[T any](v T) *T { return &v }

// newService returns a capture service on a real database, with one user.
func newService(t *testing.T) (*capture.Service, string) {
	t.Helper()
	return newServiceAt(t, time.Now)
}

// newServiceAt is newService with a store that reads the time from now, and the Paris zone.
func newServiceAt(t *testing.T, now func() time.Time) (*capture.Service, string) {
	t.Helper()
	st, _ := storetest.OpenWithClock(t, now)
	userID, err := st.CreateUser(context.Background(), "owner", "hash")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	paris, err := capture.LoadParis()
	if err != nil {
		t.Fatalf("LoadParis: %v", err)
	}
	return capture.NewService(st, paris), userID
}

func TestCaptureRefusals(t *testing.T) {
	cases := []struct {
		name    string
		req     capture.Request
		wantErr error  // a sentinel error, or nil when field is set
		field   string // field of the ValidationError
	}{
		{name: "empty client_id", req: capture.Request{URL: ptr("https://example.com/a"), ClientID: ptr("")}, field: "client_id"},
		{name: "malformed client_id", req: capture.Request{URL: ptr("https://example.com/a"), ClientID: ptr("not-a-ulid")}, field: "client_id"},
		{name: "lower-case client_id", req: capture.Request{URL: ptr("https://example.com/a"), ClientID: ptr(strings.ToLower(testClientID))}, field: "client_id"},
		{name: "client_id with surrounding spaces", req: capture.Request{URL: ptr("https://example.com/a"), ClientID: ptr(" " + testClientID)}, field: "client_id"},
		{name: "unknown source", req: capture.Request{URL: ptr("https://example.com/a"), Source: ptr("rss")}, field: "source"},
		{name: "empty source", req: capture.Request{URL: ptr("https://example.com/a"), Source: ptr("")}, field: "source"},
		{name: "client_id checked before ambiguity", req: capture.Request{URL: ptr("https://example.com/a"), Note: ptr("x"), ClientID: ptr("bad")}, field: "client_id"},

		{name: "url and note", req: capture.Request{URL: ptr("https://example.com/a"), Note: ptr("buy milk")}, wantErr: capture.ErrAmbiguousCapture},
		{name: "note and user_note", req: capture.Request{Note: ptr("buy milk"), UserNote: ptr("today")}, wantErr: capture.ErrAmbiguousCapture},
		{name: "ambiguity wins over an invalid url", req: capture.Request{URL: ptr("not a url"), Note: ptr("buy milk")}, wantErr: capture.ErrAmbiguousCapture},
		{name: "ambiguity wins over an empty url", req: capture.Request{URL: ptr(""), Note: ptr("buy milk")}, wantErr: capture.ErrAmbiguousCapture},
		{name: "blank user_note with a note is a note capture", req: capture.Request{Note: ptr("buy milk"), UserNote: ptr("   ")}, wantErr: capture.ErrNoteNotImplemented},
		{name: "note alone is not implemented", req: capture.Request{Note: ptr("buy milk")}, wantErr: capture.ErrNoteNotImplemented},

		{name: "empty capture", req: capture.Request{}, wantErr: capture.ErrEmptyCapture},
		{name: "user_note alone", req: capture.Request{UserNote: ptr("a thought")}, wantErr: capture.ErrEmptyCapture},
		{name: "private flag alone", req: capture.Request{IsPrivate: ptr(true)}, wantErr: capture.ErrEmptyCapture},

		{name: "blank url", req: capture.Request{URL: ptr("   ")}, wantErr: capture.ErrInvalidURL},
		{name: "empty url", req: capture.Request{URL: ptr("")}, wantErr: capture.ErrInvalidURL},
		{name: "javascript scheme", req: capture.Request{URL: ptr("javascript:alert(1)")}, wantErr: capture.ErrInvalidURL},
		{name: "data scheme", req: capture.Request{URL: ptr("data:text/html,hi")}, wantErr: capture.ErrInvalidURL},
		{name: "mailto scheme", req: capture.Request{URL: ptr("mailto:someone@example.com")}, wantErr: capture.ErrInvalidURL},
		{name: "ftp scheme", req: capture.Request{URL: ptr("ftp://example.com/file")}, wantErr: capture.ErrInvalidURL},
		{name: "relative url", req: capture.Request{URL: ptr("example.com/article")}, wantErr: capture.ErrInvalidURL},
		{name: "scheme without host", req: capture.Request{URL: ptr("https://")}, wantErr: capture.ErrInvalidURL},
		{name: "opaque url", req: capture.Request{URL: ptr("https:example.com")}, wantErr: capture.ErrInvalidURL},
		{name: "port without host", req: capture.Request{URL: ptr("https://:443/a")}, wantErr: capture.ErrInvalidURL},
		{name: "malformed escape", req: capture.Request{URL: ptr("https://example.com/%zz")}, wantErr: capture.ErrInvalidURL},
		{name: "space in host", req: capture.Request{URL: ptr("https://exa mple.com/")}, wantErr: capture.ErrInvalidURL},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service, userID := newService(t)
			_, err := service.Capture(context.Background(), userID, tc.req)
			if tc.field != "" {
				var validation *capture.ValidationError
				if !errors.As(err, &validation) || validation.Field != tc.field {
					t.Fatalf("err = %v, want a ValidationError on %s", err, tc.field)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestCaptureLink(t *testing.T) {
	ctx := context.Background()

	captureOK := func(t *testing.T, req capture.Request) store.Item {
		t.Helper()
		service, userID := newService(t)
		result, err := service.Capture(ctx, userID, req)
		if err != nil {
			t.Fatalf("Capture: %v", err)
		}
		if !result.Created {
			t.Fatal("Created = false, want true")
		}
		return result.Item
	}

	t.Run("a link becomes a queued link item with defaults", func(t *testing.T) {
		item := captureOK(t, capture.Request{URL: ptr("https://example.com/article")})
		if item.Kind != "link" || item.Status != "queued" || item.Source != "share" || item.IsPrivate || item.UserNote != nil {
			t.Errorf("item = %+v, want a queued public link from share without note", item)
		}
		if item.URL == nil || *item.URL != "https://example.com/article" {
			t.Errorf("url = %v, want https://example.com/article", item.URL)
		}
	})

	t.Run("note, privacy and source are recorded", func(t *testing.T) {
		item := captureOK(t, capture.Request{URL: ptr("https://example.com/a"), UserNote: ptr("for the trip"), IsPrivate: ptr(true), Source: ptr("manual")})
		if item.UserNote == nil || *item.UserNote != "for the trip" || !item.IsPrivate || item.Source != "manual" {
			t.Errorf("item = %+v, want the note, private and manual", item)
		}
	})

	t.Run("surrounding whitespace is trimmed from url and user_note", func(t *testing.T) {
		item := captureOK(t, capture.Request{URL: ptr("  https://example.com/a\n"), UserNote: ptr("\t for the trip \n")})
		if *item.URL != "https://example.com/a" || *item.UserNote != "for the trip" {
			t.Errorf("url, user_note = %q, %q; want them trimmed", *item.URL, *item.UserNote)
		}
	})

	t.Run("a blank user_note is absent", func(t *testing.T) {
		item := captureOK(t, capture.Request{URL: ptr("https://example.com/a"), UserNote: ptr("  \n ")})
		if item.UserNote != nil {
			t.Errorf("user_note = %q, want nil", *item.UserNote)
		}
	})

	t.Run("an upper-case scheme is accepted and the url kept as sent", func(t *testing.T) {
		item := captureOK(t, capture.Request{URL: ptr("HTTPS://Example.com/A")})
		if *item.URL != "HTTPS://Example.com/A" || item.Title != "example.com" {
			t.Errorf("url, title = %q, %q; want HTTPS://Example.com/A, example.com", *item.URL, item.Title)
		}
	})

	t.Run("user info is accepted", func(t *testing.T) {
		item := captureOK(t, capture.Request{URL: ptr("https://user:pass@example.com/a")})
		if item.Title != "example.com" {
			t.Errorf("title = %q, want example.com", item.Title)
		}
	})

	titles := []struct{ url, want string }{
		{"https://Blog.Example.com:8443/posts/1?x=1", "blog.example.com"},
		{"http://www.example.com/", "www.example.com"},
		{"http://[2001:DB8::1]:8080/a", "2001:db8::1"},
		{"https://example.com", "example.com"},
	}
	for _, tc := range titles {
		t.Run("provisional title of "+tc.url, func(t *testing.T) {
			if item := captureOK(t, capture.Request{URL: ptr(tc.url)}); item.Title != tc.want {
				t.Errorf("title = %q, want %q", item.Title, tc.want)
			}
		})
	}

	t.Run("a refused capture stores nothing", func(t *testing.T) {
		service, userID := newService(t)
		if _, err := service.Capture(ctx, userID, capture.Request{URL: ptr("ftp://example.com"), ClientID: ptr(testClientID)}); !errors.Is(err, capture.ErrInvalidURL) {
			t.Fatalf("refused capture: %v, want ErrInvalidURL", err)
		}
		result, err := service.Capture(ctx, userID, capture.Request{URL: ptr("https://example.com/a"), ClientID: ptr(testClientID)})
		if err != nil || !result.Created {
			t.Errorf("valid capture after a refused one: created %v, %v; want a new item", result.Created, err)
		}
	})

	t.Run("a replay returns the first item", func(t *testing.T) {
		service, userID := newService(t)
		first, err := service.Capture(ctx, userID, capture.Request{URL: ptr("https://example.com/a"), ClientID: ptr(testClientID)})
		if err != nil {
			t.Fatalf("Capture: %v", err)
		}
		replay, err := service.Capture(ctx, userID, capture.Request{URL: ptr("https://example.com/b"), UserNote: ptr("other"), ClientID: ptr(testClientID)})
		if err != nil {
			t.Fatalf("replay: %v", err)
		}
		if replay.Created || replay.Item.ID != first.Item.ID || *replay.Item.URL != "https://example.com/a" || replay.Item.UserNote != nil {
			t.Errorf("replay = %+v, want the first item unchanged", replay)
		}
	})
}

func TestInvalidURLErrorHidesTheURL(t *testing.T) {
	for _, raw := range []string{"https://secret.example.com/%zz", "ftp://secret.example.com/", "https://secret example.com/", "secret.example.com/path"} {
		t.Run(raw, func(t *testing.T) {
			service, userID := newService(t)
			_, err := service.Capture(context.Background(), userID, capture.Request{URL: ptr(raw), UserNote: ptr("private thought")})
			if !errors.Is(err, capture.ErrInvalidURL) {
				t.Fatalf("err = %v, want ErrInvalidURL", err)
			}
			if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "private thought") {
				t.Errorf("error %q carries the captured content", err)
			}
		})
	}
}

func TestCaptureDuplicate(t *testing.T) {
	ctx := context.Background()
	// 23:30 on 27 October 2026 in Paris (winter time, UTC+1).
	at := time.Date(2026, 10, 27, 22, 30, 0, 0, time.UTC)
	fixed := func() time.Time { return at }

	t.Run("a link with tracking parameters is the duplicate of the stored link", func(t *testing.T) {
		service, userID := newServiceAt(t, fixed)
		first, err := service.Capture(ctx, userID, capture.Request{URL: ptr("https://example.com/article?id=3")})
		if err != nil {
			t.Fatalf("Capture: %v", err)
		}
		duplicate, err := service.Capture(ctx, userID, capture.Request{URL: ptr(" HTTPS://Example.com/article/?utm_source=x&id=3#top ")})
		if err != nil {
			t.Fatalf("duplicate: %v", err)
		}
		if duplicate.Created || duplicate.Item.ID != first.Item.ID || *duplicate.Item.URL != "https://example.com/article?id=3" {
			t.Errorf("duplicate = %+v, want the first item, url as first sent", duplicate)
		}
	})

	t.Run("the note of a duplicate is trimmed and dated in Paris", func(t *testing.T) {
		service, userID := newServiceAt(t, fixed)
		if _, err := service.Capture(ctx, userID, capture.Request{URL: ptr("https://example.com/a"), UserNote: ptr("for the trip")}); err != nil {
			t.Fatalf("Capture: %v", err)
		}
		duplicate, err := service.Capture(ctx, userID, capture.Request{URL: ptr("https://example.com/a"), UserNote: ptr("  to test ")})
		if err != nil {
			t.Fatalf("duplicate: %v", err)
		}
		if want := "for the trip\n\n2026-10-27 — to test"; duplicate.Item.UserNote == nil || *duplicate.Item.UserNote != want {
			t.Errorf("user_note = %v, want %q", deref(duplicate.Item.UserNote), want)
		}
	})

	t.Run("a duplicate without note or with a blank note keeps the note", func(t *testing.T) {
		service, userID := newServiceAt(t, fixed)
		first, err := service.Capture(ctx, userID, capture.Request{URL: ptr("https://example.com/a"), UserNote: ptr("for the trip")})
		if err != nil {
			t.Fatalf("Capture: %v", err)
		}
		for _, note := range []*string{nil, ptr("  ")} {
			duplicate, err := service.Capture(ctx, userID, capture.Request{URL: ptr("https://example.com/a"), UserNote: note})
			if err != nil {
				t.Fatalf("duplicate: %v", err)
			}
			if *duplicate.Item.UserNote != "for the trip" || duplicate.Item.ChangeSeq != first.Item.ChangeSeq {
				t.Errorf("duplicate = %+v, want the item unchanged", duplicate.Item)
			}
		}
	})
}
