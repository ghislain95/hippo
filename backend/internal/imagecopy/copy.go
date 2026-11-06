package imagecopy

import (
	"bufio"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png" // PNG, registered for image.DecodeConfig and image.Decode
	"io"

	"github.com/gen2brain/h265/heic" // HEIC and HEIF stills, grids included; registers "heic"
	_ "golang.org/x/image/webp"      // WebP, registered for image.DecodeConfig and image.Decode
)

// formatHEIC, formatJPEG and formatWebP are the names image.DecodeConfig gives the formats this package
// reads apart.
const (
	formatHEIC = "heic"
	formatJPEG = "jpeg"
	formatWebP = "webp"
)

// errOutputFull: the copy reached the bound of the request.
var errOutputFull = errors.New("copy above its bound")

// defaultRequest is the request of the production limits.
func defaultRequest() request {
	return request{
		MaxPixels: MaxPixels, LongEdge: LongEdge, Quality: Quality, MaxOutput: MaxOutput,
		MemoryLimit: ChildMemoryLimit,
	}
}

// copyImage writes to dst the copy of the image of src (size bytes) for req, in this order:
// header and pixel ceiling, decoding, scaling, orientation, JPEG encoding. GIF and AVIF are not
// registered: capture never stores them (RG-02). The error is a failure to write dst; every
// outcome of the image itself is the answer's reason.
func copyImage(src io.ReaderAt, size int64, dst io.Writer, req request) (answer, error) {
	cfg, format, ok := header(src, size)
	if !ok {
		return answer{Reason: ReasonUndecodable}, nil
	}
	if int64(cfg.Width)*int64(cfg.Height) > req.MaxPixels {
		return answer{Reason: ReasonTooManyPixels}, nil
	}
	img, conv, orientations, ok := decode(src, size, format, req.MaxPixels)
	if !ok {
		return answer{Reason: ReasonUndecodable}, nil
	}
	if b := img.Bounds(); int64(b.Dx())*int64(b.Dy()) > req.MaxPixels {
		return answer{Reason: ReasonTooManyPixels}, nil
	}

	out := scale(img, conv, req.LongEdge)
	for _, o := range orientations {
		out = orient(out, o)
	}

	limited := &boundedWriter{w: dst, left: req.MaxOutput}
	buffered := bufio.NewWriter(limited)
	err := jpeg.Encode(buffered, out, &jpeg.Options{Quality: req.Quality})
	if err == nil {
		err = buffered.Flush()
	}
	switch {
	case errors.Is(err, errOutputFull):
		return answer{Reason: ReasonTooLarge}, nil
	case err != nil:
		return answer{}, fmt.Errorf("write the copy: %w", err)
	}
	return answer{Reason: ReasonOK, Width: out.Bounds().Dx(), Height: out.Bounds().Dy()}, nil
}

// boundedWriter writes to w up to left bytes, and fails with errOutputFull beyond.
type boundedWriter struct {
	w    io.Writer
	left int64
}

func (b *boundedWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > b.left {
		n, err := b.w.Write(p[:b.left])
		b.left -= int64(n)
		if err != nil {
			return n, err
		}
		return n, errOutputFull
	}
	n, err := b.w.Write(p)
	b.left -= int64(n)
	return n, err
}

// header reads the dimensions and the format of the image of src from its header; ok is false
// for anything that is not a registered format with a positive size.
func header(src io.ReaderAt, size int64) (cfg image.Config, format string, ok bool) {
	cfg, format, err := image.DecodeConfig(io.NewSectionReader(src, 0, size))
	return cfg, format, err == nil && cfg.Width > 0 && cfg.Height > 0
}

// decode decodes the image of src in format, with the conversion of its Y′CbCr samples and the
// EXIF orientations that turn it upright; ok is false when it cannot be decoded. A HEIC's
// coded frames are bounded by maxPixels: its header gives the size of the image, not of the
// frames the decoder allocates (design.md D10).
func decode(src io.ReaderAt, size int64, format string, maxPixels int64) (img image.Image, conv converter, orientations []int, ok bool) {
	var err error
	switch format {
	case formatHEIC:
		// Y′CbCr as decoded, not NRGBA: 1.5 bytes a pixel instead of 4 (design.md D10).
		var info heic.ColorInfo
		img, info, err = heic.DecodeColor(io.NewSectionReader(src, 0, size),
			heic.Options{ToYCbCr: true, FrameSizeLimit: int(maxPixels)})
		conv, ok = converterFor(info.Matrix, info.FullRange)
		return img, conv, heifTransform(src, size).orientations(), err == nil && ok
	case formatJPEG:
		img, err = jpeg.Decode(io.NewSectionReader(src, 0, size))
		return img, jfif, []int{jpegOrientation(src, size)}, err == nil
	case formatWebP:
		img, _, err = image.Decode(io.NewSectionReader(src, 0, size))
		return img, vp8, nil, err == nil
	}
	img, _, err = image.Decode(io.NewSectionReader(src, 0, size))
	return img, jfif, nil, err == nil
}
