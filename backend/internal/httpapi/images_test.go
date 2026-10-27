package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/ghislain95/hippo/backend/internal/capture/imagetest"
)

const (
	imageClientID        = "01JB8Z6Q4M1X2Y3Z4A5B6C7D8J"
	unsupportedImageType = "urn:hippo:problem:unsupported-image"
	imageTooLargeType    = "urn:hippo:problem:image-too-large"
	maxImage             = 20 * 1024 * 1024
	assetCacheControl    = "private, max-age=31536000, immutable"
)

// part is one part of a multipart capture: a file when filename is set, a text field otherwise.
type part struct {
	name        string
	filename    string
	contentType string
	content     []byte
}

func imagePart(content []byte) part {
	return part{name: "image", filename: "capture.png", contentType: "image/png", content: content}
}

func field(name, value string) part {
	return part{name: name, content: []byte(value)}
}

// multipartBody encodes parts, in order, as a multipart/form-data body.
func multipartBody(t testing.TB, parts ...part) (contentType string, body []byte) {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	for _, p := range parts {
		header := textproto.MIMEHeader{}
		disposition := fmt.Sprintf(`form-data; name=%q`, p.name)
		if p.filename != "" {
			disposition += fmt.Sprintf(`; filename=%q`, p.filename)
		}
		header.Set("Content-Disposition", disposition)
		if p.contentType != "" {
			header.Set("Content-Type", p.contentType)
		}
		w, err := writer.CreatePart(header)
		if err != nil {
			t.Fatalf("create part: %v", err)
		}
		if _, err := w.Write(p.content); err != nil {
			t.Fatalf("write part: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}
	return writer.FormDataContentType(), buf.Bytes()
}

// captureImage sends POST /api/items with a multipart body of parts.
func (f fixture) captureImage(t *testing.T, token string, parts ...part) *http.Response {
	t.Helper()
	contentType, body := multipartBody(t, parts...)
	return f.do(t, http.MethodPost, "/api/items", token, contentType, string(body))
}

// assetFiles lists every file of the assets directory, temporary files included.
func (f fixture) assetFiles(t *testing.T) []string {
	t.Helper()
	var found []string
	err := filepath.WalkDir(f.assetsDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			rel, err := filepath.Rel(f.assetsDir, path)
			if err != nil {
				return err
			}
			found = append(found, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk assets directory: %v", err)
	}
	sort.Strings(found)
	return found
}

// assertNothingStored checks that a refused capture left no item and no file.
func (f fixture) assertNothingStored(t *testing.T) {
	t.Helper()
	if got := f.itemCount(t, f.userID); got != 0 {
		t.Errorf("items = %d, want 0", got)
	}
	if got := f.assetFiles(t); len(got) != 0 {
		t.Errorf("files left in the data directory: %v", got)
	}
}

// singleAsset returns the only asset of an item answer.
func singleAsset(t *testing.T, item map[string]any) map[string]any {
	t.Helper()
	assets, ok := item["assets"].([]any)
	if !ok || len(assets) != 1 {
		t.Fatalf("assets = %#v, want one asset", item["assets"])
	}
	asset, ok := assets[0].(map[string]any)
	if !ok {
		t.Fatalf("asset = %#v, want an object", assets[0])
	}
	return asset
}

func validateItem(t *testing.T, raw []byte) {
	t.Helper()
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("parse body: %v", err)
	}
	if err := itemSchema(t).Validate(instance); err != nil {
		t.Errorf("body does not match the Item schema: %v\n%s", err, raw)
	}
}

// TestImageCapture follows the image requirements of
// openspec/changes/capture-image/specs/item-capture/spec.md: one subtest per scenario, named
// after it, grouped by requirement. Every refusal checks that no item and no file is left.
func TestImageCapture(t *testing.T) {
	t.Run("An image capture creates a queued image item with its asset", func(t *testing.T) {
		t.Run("Capture a PNG screenshot", func(t *testing.T) {
			f := newFixture(t)
			png := imagetest.PNG(640, 480)
			resp := f.captureImage(t, testToken, imagePart(png), field("user_note", "recipe for Sunday"),
				field("is_private", "true"), field("source", "manual"))
			_, item := capturedItem(t, resp, http.StatusCreated)
			assertMembers(t, item, map[string]any{
				"kind": "image", "status": "queued", "read_status": "to_read", "url": nil,
				"user_note": "recipe for Sunday", "is_private": true, "source": "manual",
			})
			asset := singleAsset(t, item)
			assertMembers(t, asset, map[string]any{
				"mime_type": "image/png", "width": float64(640), "height": float64(480), "size_bytes": float64(len(png)),
			})
			if id, _ := asset["id"].(string); !ulidPattern.MatchString(id) {
				t.Errorf("asset id = %v, want a ULID", asset["id"])
			}
			if got := f.assetFiles(t); len(got) != 1 {
				t.Errorf("files = %v, want one image file", got)
			}
		})

		t.Run("Capture a JPEG photo with defaults", func(t *testing.T) {
			f := newFixture(t)
			_, item := capturedItem(t, f.captureImage(t, testToken, imagePart(imagetest.JPEG(48, 32))), http.StatusCreated)
			assertMembers(t, item, map[string]any{"user_note": nil, "is_private": false, "source": "share"})
			assertMembers(t, singleAsset(t, item), map[string]any{"mime_type": "image/jpeg", "width": float64(48), "height": float64(32)})
		})

		t.Run("HEIC and WebP are accepted without dimensions", func(t *testing.T) {
			f := newFixture(t)
			for _, tc := range []struct {
				content  []byte
				mimeType string
			}{{imagetest.HEIC("heic", "mif1", "heic"), "image/heic"}, {imagetest.WebP("VP8 "), "image/webp"}} {
				_, item := capturedItem(t, f.captureImage(t, testToken, imagePart(tc.content)), http.StatusCreated)
				assertMembers(t, singleAsset(t, item), map[string]any{"mime_type": tc.mimeType, "width": nil, "height": nil})
			}
		})

		t.Run("Same image twice makes two items", func(t *testing.T) {
			f := newFixture(t)
			png := imagetest.PNG(16, 16)
			_, first := capturedItem(t, f.captureImage(t, testToken, imagePart(png)), http.StatusCreated)
			_, second := capturedItem(t, f.captureImage(t, testToken, imagePart(png)), http.StatusCreated)
			if first["id"] == second["id"] || singleAsset(t, first)["id"] == singleAsset(t, second)["id"] {
				t.Errorf("items %v and %v, want different item and asset ids", first["id"], second["id"])
			}
			if got := f.assetFiles(t); len(got) != 2 {
				t.Errorf("files = %v, want two", got)
			}
		})

		t.Run("The answer matches the contract for an image", func(t *testing.T) {
			f := newFixture(t)
			for _, content := range [][]byte{imagetest.PNG(8, 8), imagetest.HEIC("heic")} {
				raw, item := capturedItem(t, f.captureImage(t, testToken, imagePart(content)), http.StatusCreated)
				validateItem(t, raw)
				assertEmptyArray(t, item, "tag_ids")
				assertEmptyArray(t, item, "user_edited")
			}
		})
	})

	t.Run("The image format is recognised from its content", func(t *testing.T) {
		t.Run("Unsupported format", func(t *testing.T) {
			for _, tc := range []struct {
				name    string
				content []byte
			}{
				{"GIF", imagetest.GIF()},
				{"SVG", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"></svg>`)},
				{"AVIF", imagetest.AVIF()},
				{"text", []byte("not an image at all\n")},
			} {
				t.Run(tc.name, func(t *testing.T) {
					f := newFixture(t)
					assertProblemType(t, f.captureImage(t, testToken, imagePart(tc.content)), http.StatusBadRequest, unsupportedImageType)
					f.assertNothingStored(t)
				})
			}
		})

		t.Run("Truncated JPEG or PNG", func(t *testing.T) {
			for name, content := range map[string][]byte{"JPEG": imagetest.JPEG(16, 16)[:24], "PNG": imagetest.PNG(16, 16)[:24]} {
				t.Run(name, func(t *testing.T) {
					f := newFixture(t)
					assertProblemType(t, f.captureImage(t, testToken, imagePart(content)), http.StatusBadRequest, unsupportedImageType)
					f.assertNothingStored(t)
				})
			}
		})

		t.Run("Declared type is ignored", func(t *testing.T) {
			f := newFixture(t)
			declaredJPEG := part{name: "image", filename: "photo.jpg", contentType: "image/jpeg", content: imagetest.PNG(8, 8)}
			_, item := capturedItem(t, f.captureImage(t, testToken, declaredJPEG), http.StatusCreated)
			assertMembers(t, singleAsset(t, item), map[string]any{"mime_type": "image/png"})
			if got := f.assetFiles(t); len(got) != 1 || !strings.HasSuffix(got[0], ".png") {
				t.Errorf("files = %v, want one .png file", got)
			}
		})
	})

	t.Run("An image is 20 MB at most", func(t *testing.T) {
		t.Run("Image just at the limit", func(t *testing.T) {
			f := newFixture(t)
			_, item := capturedItem(t, f.captureImage(t, testToken, imagePart(imagetest.PNGOfSize(8, 8, maxImage))), http.StatusCreated)
			assertMembers(t, singleAsset(t, item), map[string]any{"size_bytes": float64(maxImage)})
		})

		t.Run("Image over the limit", func(t *testing.T) {
			f := newFixture(t)
			resp := f.captureImage(t, testToken, imagePart(imagetest.PNGOfSize(8, 8, maxImage+1)))
			assertProblemType(t, resp, http.StatusRequestEntityTooLarge, imageTooLargeType)
			f.assertNothingStored(t)
		})

		// The order of the checks does not depend on the order of the parts (design, C1).
		t.Run("an oversized image wins over an undeclared field sent before it", func(t *testing.T) {
			f := newFixture(t)
			resp := f.captureImage(t, testToken, field("url", "https://example.com/a"), imagePart(imagetest.PNGOfSize(8, 8, maxImage+1)))
			assertProblemType(t, resp, http.StatusRequestEntityTooLarge, imageTooLargeType)
			f.assertNothingStored(t)
		})
	})

	t.Run("The fields of an image capture follow the capture rules", func(t *testing.T) {
		t.Run("Blank capture note", func(t *testing.T) {
			f := newFixture(t)
			_, item := capturedItem(t, f.captureImage(t, testToken, imagePart(imagetest.PNG(8, 8)), field("user_note", "  \n ")), http.StatusCreated)
			assertMembers(t, item, map[string]any{"user_note": nil})
		})

		t.Run("Missing image", func(t *testing.T) {
			f := newFixture(t)
			assertProblemType(t, f.captureImage(t, testToken, field("user_note", "a thought")), http.StatusBadRequest, blankType)
			f.assertNothingStored(t)
		})

		t.Run("Two images", func(t *testing.T) {
			f := newFixture(t)
			resp := f.captureImage(t, testToken, imagePart(imagetest.PNG(8, 8)), imagePart(imagetest.JPEG(8, 8)))
			assertProblemType(t, resp, http.StatusBadRequest, blankType)
			f.assertNothingStored(t)
		})

		t.Run("Undeclared or malformed field", func(t *testing.T) {
			for _, tc := range []struct {
				name  string
				field part
			}{
				{"field isPrivate", field("isPrivate", "true")},
				{"is_private yes", field("is_private", "yes")},
				{"is_private 1", field("is_private", "1")},
				{"is_private empty", field("is_private", "")},
				{"client_id not-a-ulid", field("client_id", "not-a-ulid")},
				{"source rss", field("source", "rss")},
				{"note field", field("note", "buy milk")},
				{"url field", field("url", "https://example.com/a")},
				{"repeated user_note", field("user_note", "once")},
				{"text field with a file name", part{name: "user_note", filename: "note.txt", content: []byte("hi")}},
				{"invalid UTF-8", field("user_note", "caf\xe9")},
				{"text field over 1 MiB", field("user_note", strings.Repeat("a", 1<<20+1))},
			} {
				t.Run(tc.name, func(t *testing.T) {
					f := newFixture(t)
					parts := []part{imagePart(imagetest.PNG(8, 8)), tc.field}
					if tc.name == "repeated user_note" {
						parts = append(parts, field("user_note", "twice"))
					}
					assertProblemType(t, f.captureImage(t, testToken, parts...), http.StatusBadRequest, blankType)
					f.assertNothingStored(t)
				})
			}
		})

		t.Run("Fields after the file", func(t *testing.T) {
			f := newFixture(t)
			parts := []part{
				imagePart(imagetest.PNG(8, 8)), field("client_id", imageClientID), field("user_note", "after the file"), field("source", "manual"),
			}
			_, item := capturedItem(t, f.captureImage(t, testToken, parts...), http.StatusCreated)
			assertMembers(t, item, map[string]any{"user_note": "after the file", "source": "manual"})
			_, replay := capturedItem(t, f.captureImage(t, testToken, parts...), http.StatusOK)
			assertMembers(t, replay, map[string]any{"id": item["id"]})
		})

		t.Run("Oversized multipart body", func(t *testing.T) {
			f := newFixture(t)
			resp := f.captureImage(t, testToken, imagePart(imagetest.PNG(8, 8)), field("user_note", strings.Repeat("a", 21<<20)))
			assertProblemType(t, resp, http.StatusRequestEntityTooLarge, blankType)
			f.assertNothingStored(t)
		})

		// Same rule without a declared Content-Length: the body limit hits while reading.
		t.Run("oversized multipart body without Content-Length", func(t *testing.T) {
			f := newFixture(t)
			contentType, body := multipartBody(t, imagePart(imagetest.PNG(8, 8)), field("user_note", strings.Repeat("a", 21<<20)))
			resp := f.sendChunked(t, contentType, body)
			assertProblemType(t, resp, http.StatusRequestEntityTooLarge, blankType)
			f.assertNothingStored(t)
		})

		// A truncated body (client gone mid-upload) is refused, and its file removed.
		t.Run("truncated multipart body", func(t *testing.T) {
			f := newFixture(t)
			contentType, body := multipartBody(t, imagePart(imagetest.PNG(64, 64)))
			resp := f.do(t, http.MethodPost, "/api/items", testToken, contentType, string(body[:len(body)/2]))
			assertProblemType(t, resp, http.StatusBadRequest, blankType)
			f.assertNothingStored(t)
			if strings.Contains(f.logs.String(), `"level":"ERROR"`) {
				t.Errorf("a refused body was logged at ERROR:\n%s", f.logs.String())
			}
		})
	})

	t.Run("A replayed image capture stores no new image", func(t *testing.T) {
		t.Run("Replay of an image capture", func(t *testing.T) {
			f := newFixture(t)
			parts := []part{imagePart(imagetest.PNG(8, 8)), field("client_id", imageClientID)}
			first, item := capturedItem(t, f.captureImage(t, testToken, parts...), http.StatusCreated)
			replay, _ := capturedItem(t, f.captureImage(t, testToken, parts...), http.StatusOK)
			if !bytes.Equal(replay, first) {
				t.Errorf("replay = %s\nwant the same item and asset %s", replay, first)
			}
			assetID, _ := singleAsset(t, item)["id"].(string)
			if got := f.assetFiles(t); len(got) != 1 || got[0] != f.userID+"/"+assetID+".png" {
				t.Errorf("files = %v, want only the file of asset %s", got, assetID)
			}
			if got := f.itemCount(t, f.userID); got != 1 {
				t.Errorf("items = %d, want 1", got)
			}
		})

		t.Run("Concurrent replays of an image capture", func(t *testing.T) {
			f := newFixture(t)
			const replays = 8
			contentType, body := multipartBody(t, imagePart(imagetest.PNG(32, 32)), field("client_id", imageClientID))
			statuses, ids, errs := make([]int, replays), make([]string, replays), make([]error, replays)
			var wg sync.WaitGroup
			for i := range replays {
				wg.Go(func() { statuses[i], ids[i], errs[i] = sendConcurrently(f, contentType, body) })
			}
			wg.Wait()
			created := 0
			for i := range replays {
				if errs[i] != nil {
					t.Fatalf("replay %d: %v", i, errs[i])
				}
				switch statuses[i] {
				case http.StatusCreated:
					created++
				case http.StatusOK:
				default:
					t.Errorf("replay %d: status %d, want 201 or 200", i, statuses[i])
				}
				if ids[i] != ids[0] {
					t.Errorf("replay %d: id %s, want %s", i, ids[i], ids[0])
				}
			}
			if created != 1 {
				t.Errorf("%d answers are 201, want exactly 1", created)
			}
			if got := f.assetFiles(t); len(got) != 1 {
				t.Errorf("files = %v, want one image file", got)
			}
		})
	})

	t.Run("The provisional title of an image", func(t *testing.T) {
		t.Run("Title from the capture note", func(t *testing.T) {
			f := newFixture(t)
			resp := f.captureImage(t, testToken, imagePart(imagetest.PNG(8, 8)), field("user_note", "Tarte aux pommes\nde mamie"))
			_, item := capturedItem(t, resp, http.StatusCreated)
			assertMembers(t, item, map[string]any{"title": "Tarte aux pommes"})
		})

		t.Run("Dated title without note", func(t *testing.T) {
			f := newFixture(t)
			f.clock.Set(time.Date(2026, 10, 27, 23, 30, 0, 0, time.UTC))
			_, item := capturedItem(t, f.captureImage(t, testToken, imagePart(imagetest.PNG(8, 8))), http.StatusCreated)
			assertMembers(t, item, map[string]any{"title": "Image du 28 octobre 2026"})
		})

		t.Run("First day of the month", func(t *testing.T) {
			f := newFixture(t)
			f.clock.Set(time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC))
			_, item := capturedItem(t, f.captureImage(t, testToken, imagePart(imagetest.PNG(8, 8))), http.StatusCreated)
			assertMembers(t, item, map[string]any{"title": "Image du 1er novembre 2026"})
		})
	})

	t.Run("An image capture keeps its content out of the logs", func(t *testing.T) {
		t.Run("Image capture logs no content", func(t *testing.T) {
			f := newFixture(t)
			secret := part{name: "image", filename: "secret-plan.png", contentType: "image/png", content: imagetest.PNG(8, 8)}
			capturedItem(t, f.captureImage(t, testToken, secret, field("user_note", "private thought")), http.StatusCreated)
			// The refusals log nothing of the request either.
			gif := part{name: "image", filename: "secret-plan.gif", content: imagetest.GIF()}
			assertProblemType(t, f.captureImage(t, testToken, gif, field("user_note", "private thought")), http.StatusBadRequest, unsupportedImageType)
			assertProblemType(t, f.captureImage(t, testToken, secret, field("secret-plan", "private thought")), http.StatusBadRequest, blankType)
			logs := f.logs.String()
			for _, leak := range []string{"secret-plan", "private thought"} {
				if strings.Contains(logs, leak) {
					t.Errorf("logs contain %q:\n%s", leak, logs)
				}
			}
			if !strings.Contains(logs, `"status":201`) {
				t.Errorf("logs have no request line with status 201:\n%s", logs)
			}
		})
	})
}

// TestImageAssets follows openspec/changes/capture-image/specs/image-assets/spec.md: one
// subtest per scenario, named after it, grouped by requirement.
func TestImageAssets(t *testing.T) {
	t.Run("An asset is served to its owner as captured", func(t *testing.T) {
		t.Run("Download a captured image", func(t *testing.T) {
			f := newFixture(t)
			png := imagetest.PNG(64, 48)
			_, item := capturedItem(t, f.captureImage(t, testToken, imagePart(png)), http.StatusCreated)
			resp := f.get(t, "/api/assets/"+singleAsset(t, item)["id"].(string), testToken)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}
			for header, want := range map[string]string{
				"Content-Type": "image/png", "X-Content-Type-Options": "nosniff", "Cache-Control": assetCacheControl,
				"Content-Length": fmt.Sprint(len(png)),
			} {
				if got := resp.Header.Get(header); got != want {
					t.Errorf("%s = %q, want %q", header, got, want)
				}
			}
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			if !bytes.Equal(body, png) {
				t.Errorf("body of %d bytes differs from the %d bytes sent", len(body), len(png))
			}
		})

		t.Run("Private item's image is served to its owner", func(t *testing.T) {
			f := newFixture(t)
			heic := imagetest.HEIC("heic", "mif1", "heic")
			_, item := capturedItem(t, f.captureImage(t, testToken, imagePart(heic), field("is_private", "true")), http.StatusCreated)
			resp := f.get(t, "/api/assets/"+singleAsset(t, item)["id"].(string), testToken)
			if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/heic" {
				t.Fatalf("status %d type %q, want 200 image/heic", resp.StatusCode, resp.Header.Get("Content-Type"))
			}
			if body, err := io.ReadAll(resp.Body); err != nil || !bytes.Equal(body, heic) {
				t.Errorf("body differs from the file sent (%v)", err)
			}
		})
	})

	t.Run("Another user's or an unknown asset is not found", func(t *testing.T) {
		t.Run("Another user's asset", func(t *testing.T) {
			f := newFixture(t)
			_, item := capturedItem(t, f.captureImage(t, testToken, imagePart(imagetest.PNG(8, 8))), http.StatusCreated)
			assertProblem(t, f.get(t, "/api/assets/"+singleAsset(t, item)["id"].(string), otherToken), http.StatusNotFound)
		})

		t.Run("Unknown asset", func(t *testing.T) {
			f := newFixture(t)
			assertProblem(t, f.get(t, "/api/assets/01JB8Z6Q4M1X2Y3Z4A5B6C7D99", testToken), http.StatusNotFound)
		})

		t.Run("Asset without token", func(t *testing.T) {
			f := newFixture(t)
			_, item := capturedItem(t, f.captureImage(t, testToken, imagePart(imagetest.PNG(8, 8))), http.StatusCreated)
			assertProblem(t, f.get(t, "/api/assets/"+singleAsset(t, item)["id"].(string), ""), http.StatusUnauthorized)
		})
	})

	// The path parameter selects a row, never a file.
	t.Run("a path-like asset id is not found", func(t *testing.T) {
		f := newFixture(t)
		capturedItem(t, f.captureImage(t, testToken, imagePart(imagetest.PNG(8, 8))), http.StatusCreated)
		files := f.assetFiles(t)
		assertProblem(t, f.get(t, "/api/assets/"+strings.ReplaceAll(files[0], "/", "%2F"), testToken), http.StatusNotFound)
	})

	t.Run("a missing file answers 500, logged with the asset id only", func(t *testing.T) {
		f := newFixture(t)
		_, item := capturedItem(t, f.captureImage(t, testToken, imagePart(imagetest.PNG(8, 8))), http.StatusCreated)
		assetID := singleAsset(t, item)["id"].(string)
		if err := os.Remove(filepath.Join(f.assetsDir, f.userID, assetID+".png")); err != nil {
			t.Fatalf("remove file: %v", err)
		}
		assertProblem(t, f.get(t, "/api/assets/"+assetID, testToken), http.StatusInternalServerError)
		logs := f.logs.String()
		if !strings.Contains(logs, assetID) || strings.Contains(logs, assetID+".png") || strings.Contains(logs, f.assetsDir) {
			t.Errorf("logs should name asset %s and no path:\n%s", assetID, logs)
		}
	})
}

// TestTransferDeadlines checks, on a real server, that the per-route deadlines of an upload and
// a download are applied: http.ResponseController reaches the connection, so nothing warns.
func TestTransferDeadlines(t *testing.T) {
	t.Run("an upload and a download extend their deadlines without warning", func(t *testing.T) {
		f := newFixture(t)
		_, item := capturedItem(t, f.captureImage(t, testToken, imagePart(imagetest.PNG(8, 8))), http.StatusCreated)
		resp := f.get(t, "/api/assets/"+singleAsset(t, item)["id"].(string), testToken)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("download status = %d, want 200", resp.StatusCode)
		}
		logs := f.logs.String()
		for _, unwanted := range []string{"deadline", `"level":"WARN"`, `"level":"ERROR"`} {
			if strings.Contains(logs, unwanted) {
				t.Errorf("logs contain %s:\n%s", unwanted, logs)
			}
		}
		for _, route := range []string{`"route":"POST /api/items"`, `"route":"GET /api/assets/{asset_id}"`} {
			if !strings.Contains(logs, route) {
				t.Errorf("logs have no line for %s:\n%s", route, logs)
			}
		}
	})
}

// sendChunked sends an image capture without Content-Length: the body is streamed in chunks.
func (f fixture) sendChunked(t *testing.T, contentType string, body []byte) *http.Response {
	t.Helper()
	// io.MultiReader hides the length, so the client cannot declare it.
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, f.server.URL+"/api/items", io.MultiReader(bytes.NewReader(body)))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", contentType)
	resp, err := f.server.Client().Do(req)
	if err != nil {
		t.Fatalf("POST /api/items: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// sendConcurrently sends an image capture from a goroutine, where t.Fatal cannot be called.
func sendConcurrently(f fixture, contentType string, body []byte) (status int, id string, err error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, f.server.URL+"/api/items", bytes.NewReader(body))
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", contentType)
	resp, err := f.server.Client().Do(req)
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	var item struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&item); err != nil {
		return resp.StatusCode, "", err
	}
	return resp.StatusCode, item.ID, nil
}
