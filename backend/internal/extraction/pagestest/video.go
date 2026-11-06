package pagestest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"strings"
)

// The video pages below are fictional watch pages of YouTube built in code: their player data
// (ytInitialPlayerResponse) have the shape of a real page, with made-up values. No real page of
// YouTube is in the repository (ADR-0027).

// Video is the metadata of a fictional video.
type Video struct {
	ID, Title, Author, Description string
	// LengthSeconds is the duration as YouTube writes it, a decimal string; "" leaves it out.
	LengthSeconds string
}

// Details is the videoDetails object of v, as a map, so that a test can give a field another
// type or remove it.
func Details(v Video) map[string]any {
	d := map[string]any{
		"videoId":           v.ID,
		"title":             v.Title,
		"keywords":          []string{"fictional", "example"},
		"channelId":         "UCexampleexampleexample0",
		"isOwnerViewing":    false,
		"shortDescription":  v.Description,
		"isCrawlable":       true,
		"thumbnail":         map[string]any{"thumbnails": thumbnails(v.ID)},
		"allowRatings":      true,
		"viewCount":         "12345",
		"author":            v.Author,
		"isPrivate":         false,
		"isUnpluggedCorpus": false,
		"isLiveContent":     false,
	}
	if v.LengthSeconds != "" {
		d["lengthSeconds"] = v.LengthSeconds
	}
	return d
}

func thumbnails(id string) []map[string]any {
	var list []map[string]any
	for _, size := range [][2]int{{168, 94}, {196, 110}, {246, 138}, {336, 188}, {1920, 1080}} {
		list = append(list, map[string]any{
			"url":   fmt.Sprintf("https://i.ytimg.example/vi/%s/%dx%d.jpg", id, size[0], size[1]),
			"width": size[0], "height": size[1],
		})
	}
	return list
}

// PlayerResponse is the JSON of the player data holding details, in the shape of a watch page:
// responseContext, playabilityStatus, streamingData with formats adaptive formats, videoDetails,
// microformat. formats is the number of adaptive formats, which sets its size (about 1 KB each).
func PlayerResponse(details map[string]any, formats int) []byte {
	id, _ := details["videoId"].(string)
	adaptive := make([]map[string]any, 0, formats)
	for i := 0; i < formats; i++ {
		adaptive = append(adaptive, map[string]any{
			"itag":             136 + i,
			"url":              fmt.Sprintf("https://rr1---sn-example.googlevideo.example/videoplayback?expire=1&id=%s&itag=%d&source=youtube&requiressl=yes&mime=video%%2Fmp4&%s", id, 136+i, strings.Repeat("sig=AOq0QJ8wRQIhA", 20)),
			"mimeType":         `video/mp4; codecs="avc1.4d401f"`,
			"bitrate":          1_000_000 + i,
			"width":            1280,
			"height":           720,
			"initRange":        map[string]string{"start": "0", "end": "740"},
			"indexRange":       map[string]string{"start": "741", "end": "2000"},
			"lastModified":     "1700000000000000",
			"contentLength":    "123456789",
			"quality":          "hd720",
			"fps":              30,
			"qualityLabel":     "720p",
			"projectionType":   "RECTANGULAR",
			"averageBitrate":   900_000,
			"approxDurationMs": "754000",
		})
	}
	player := map[string]any{
		"responseContext": map[string]any{
			"serviceTrackingParams": []map[string]any{{"service": "GFEEDBACK", "params": []map[string]string{{"key": "is_viewed_live", "value": "False"}}}},
			"maxAgeSeconds":         0,
		},
		"playabilityStatus": map[string]any{"status": "OK", "playableInEmbed": true},
		"streamingData":     map[string]any{"expiresInSeconds": "21540", "adaptiveFormats": adaptive},
		"videoDetails":      details,
		"microformat": map[string]any{"playerMicroformatRenderer": map[string]any{
			"thumbnail":        map[string]any{"thumbnails": thumbnails(id)},
			"title":            map[string]any{"simpleText": details["title"]},
			"description":      map[string]any{"simpleText": details["shortDescription"]},
			"ownerChannelName": details["author"],
			"lengthSeconds":    details["lengthSeconds"],
			"category":         "Education",
			"publishDate":      "2026-01-01T00:00:00-08:00",
		}},
	}
	out, err := json.Marshal(player)
	if err != nil {
		panic(err)
	}
	return out
}

// UnavailablePlayerResponse is the player data of a video that does not exist: status ERROR,
// no videoDetails.
func UnavailablePlayerResponse() []byte {
	return []byte(`{"responseContext":{"maxAgeSeconds":0},"playabilityStatus":{"status":"ERROR","reason":"This video is unavailable"},"trackingParams":"CAAQu2kiEwj"}`)
}

// PlayerScript is the script that sets the player data as a watch page does.
func PlayerScript(player []byte) string {
	return "var ytInitialPlayerResponse = " + string(player) + ";var meta = document.createElement('meta');"
}

// VideoPage is a watch page titled title whose scripts are scripts, in order.
func VideoPage(title string, scripts ...string) []byte {
	var b bytes.Buffer
	videoHead(&b, title)
	for _, script := range scripts {
		b.WriteString("<script nonce=\"n0nce\">")
		b.WriteString(script)
		b.WriteString("</script>\n")
	}
	b.WriteString(`<div id="player"></div></body></html>`)
	return b.Bytes()
}

// LargeVideoPage is a watch page of about size bytes whose player script is player: other
// scripts of filler data come before (40 %) and after it, as on a real page.
func LargeVideoPage(title string, player []byte, size int) []byte {
	var b bytes.Buffer
	b.Grow(size + 4096)
	videoHead(&b, title)
	rest := size - b.Len() - len(player) - 400
	fillerScript(&b, "ytcfg.set", rest*2/5)
	b.WriteString("<script nonce=\"n0nce\">")
	b.WriteString(PlayerScript(player))
	b.WriteString("</script>\n")
	fillerScript(&b, "var ytInitialData = ", size-b.Len()-100)
	b.WriteString(`<div id="player"></div></body></html>`)
	return b.Bytes()
}

// videoHead is the head of a watch page: its title and a few meta tags.
func videoHead(b *bytes.Buffer, title string) {
	fmt.Fprintf(b, `<!DOCTYPE html><html lang="fr"><head><meta charset="utf-8"><title>%s - YouTube</title>`+
		`<meta property="og:site_name" content="YouTube"><meta property="og:title" content="%s">`+
		`<link rel="stylesheet" href="https://www.youtube.example/s/desktop/www-main.css"></head><body>`+"\n",
		html.EscapeString(title), html.EscapeString(title))
}

// fillerScript writes a script of about size bytes holding a JSON object of words, introduced
// by prefix.
func fillerScript(b *bytes.Buffer, prefix string, size int) {
	b.WriteString("<script nonce=\"n0nce\">")
	b.WriteString(prefix)
	b.WriteString(`({"contents":[`)
	start := b.Len()
	for i := 0; b.Len()-start < size; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(b, `{"itemSectionRenderer":{"trackingParams":"CBQQ%d","text":"%s %s %s"}}`, i,
			vocabulary[i%len(vocabulary)], vocabulary[(i+5)%len(vocabulary)], vocabulary[(i+11)%len(vocabulary)])
	}
	b.WriteString("]});</script>\n")
}
