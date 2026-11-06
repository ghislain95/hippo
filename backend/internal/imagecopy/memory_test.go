package imagecopy_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ghislain95/hippo/backend/internal/imagecopy"
)

// BenchmarkCopyInChild measures the child that copies an image (design.md D10, D15, task 3.9):
// for each image, child-held-MB is the highest memory its runtime held (0 when it exited at its
// limit before answering), child-rss-MB its resident set at its peak; copy-ms is the time of
// one copy, the spawn of the child included. The JPEG and PNG are built in code; HEIC files,
// made with sips from a synthetic PNG and never committed, are measured when listed, separated
// by commas, in IMAGECOPY_MEASURE_FILES. Run it with:
//
//	IMAGECOPY_MEASURE_FILES=/path/48mpx.heic go test -run '^$' -bench CopyInChild -benchtime 3x ./internal/imagecopy/
//
// Without -race: the race detector changes the memory of the child.
func BenchmarkCopyInChild(b *testing.B) {
	images := []struct {
		name string
		file func() []byte
	}{
		{name: "jpeg-12Mpx-4032x3024", file: func() []byte { return photoJPEG(b, 4032, 3024) }},
		{name: "png-screenshot-1290x2796", file: func() []byte { return screenshotPNG(b, 1290, 2796) }},
		{name: "jpeg-48Mpx-8064x6048", file: func() []byte { return photoJPEG(b, 8064, 6048) }},
	}
	if list := os.Getenv("IMAGECOPY_MEASURE_FILES"); list != "" {
		for _, path := range strings.Split(list, ",") {
			images = append(images, struct {
				name string
				file func() []byte
			}{name: filepath.Base(path), file: func() []byte {
				data, err := os.ReadFile(path)
				if err != nil {
					b.Fatal(err)
				}
				return data
			}})
		}
	} else {
		b.Log("IMAGECOPY_MEASURE_FILES not set: no HEIC measured")
	}
	copier := imagecopy.NewCopier(argv(imagecopy.ChildCommand), imagecopy.DefaultLimits())
	for _, img := range images {
		b.Run(img.name, func(b *testing.B) {
			dir := b.TempDir()
			path := filepath.Join(dir, "asset")
			if err := os.WriteFile(path, img.file(), 0o600); err != nil {
				b.Fatal(err)
			}
			asset, err := os.Open(path)
			if err != nil {
				b.Fatal(err)
			}
			defer func() { _ = asset.Close() }()
			out, err := os.Create(filepath.Join(dir, "copy"))
			if err != nil {
				b.Fatal(err)
			}
			defer func() { _ = out.Close() }()
			var worst imagecopy.Result
			begin := time.Now()
			for b.Loop() {
				res, err := copier.Copy(context.Background(), asset, out)
				if err != nil {
					b.Fatalf("Copy: %v", err)
				}
				if res.Reason != imagecopy.ReasonOK {
					b.Logf("reason %s", res.Reason)
				}
				worst.PeakBytes = max(worst.PeakBytes, res.PeakBytes)
				worst.MaxRSS = max(worst.MaxRSS, res.MaxRSS)
				worst.Size = res.Size
			}
			b.ReportMetric(float64(time.Since(begin).Milliseconds())/float64(b.N), "copy-ms")
			b.ReportMetric(float64(worst.PeakBytes)/(1<<20), "child-held-MB")
			b.ReportMetric(float64(worst.MaxRSS)/(1<<20), "child-rss-MB")
			b.ReportMetric(float64(worst.Size)/(1<<10), "copy-KB")
		})
	}
}

// photoJPEG is a width×height JPEG of quality 90 whose pixels vary everywhere, as a photo's do:
// a gradient with noise, so that the decoder has every coefficient to decode.
func photoJPEG(b *testing.B, width, height int) []byte {
	b.Helper()
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	rng := rand.New(rand.NewPCG(1, 2))
	for i := range img.Y {
		img.Y[i] = uint8(i%251) ^ uint8(rng.IntN(64))
	}
	for i := range img.Cb {
		img.Cb[i] = 96 + uint8(rng.IntN(64))
		img.Cr[i] = 96 + uint8(rng.IntN(64))
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		b.Fatal(err)
	}
	return buf.Bytes()
}

// screenshotPNG is a width×height PNG of flat bands and lines, as a phone screenshot's.
func screenshotPNG(b *testing.B, width, height int) []byte {
	b.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			c := color.NRGBA{R: 250, G: 250, B: 250, A: 255}
			if (y/40)%3 == 0 && x%7 < 4 {
				c = color.NRGBA{R: 30, G: 30, B: 30, A: 255} // lines of « text »
			}
			img.SetNRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		b.Fatal(err)
	}
	return buf.Bytes()
}
