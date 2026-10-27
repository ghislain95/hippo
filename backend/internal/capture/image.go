package capture

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"strconv"
	"time"

	"github.com/ghislain95/hippo/backend/internal/store"
)

// KindImage is the kind of an image item (docs/glossary.md).
const KindImage = "image"

// MaxImageSize is the largest image a capture accepts, in bytes: 20 MB (RG-02).
const MaxImageSize = 20 << 20

var (
	// ErrUnsupportedImage: the file of an image capture is not a JPEG, PNG, HEIC or WebP image
	// (RG-02).
	ErrUnsupportedImage = errors.New("capture image is not a JPEG, PNG, HEIC or WebP image")
	// ErrImageTooLarge: the file of an image capture exceeds MaxImageSize (RG-02).
	ErrImageTooLarge = errors.New("capture image exceeds 20 MB")
)

// Image is the file of an image capture, already written to the assets directory
// (assetfs.Upload). The capture renames it into place before the transaction, and keeps it
// only when a new asset references it.
type Image interface {
	io.ReaderAt
	// Size is the number of bytes of the file.
	Size() int64
	// Commit makes the file durable at rel, relative to the assets directory.
	Commit(rel string) error
	// Keep marks the file as referenced: Remove leaves it.
	Keep()
	// Remove deletes the file, wherever it is, unless kept; safe to call more than once.
	Remove() error
}

// ImageRequest is an image capture as received; a nil field is absent. Its text fields have
// already passed the structure checks of the transport (one each, UTF-8, is_private exactly
// true or false).
type ImageRequest struct {
	Image     Image
	UserNote  *string
	IsPrivate *bool
	Source    *string
	ClientID  *string
}

// CaptureImage checks and stores an image capture of userID: a new image item with one asset,
// or the item of a replayed client id (RG-08). An image is never a duplicate (RG-04). Checks,
// in order: size, client id and source, then the format, recognised from the content (RG-02).
//
// The file is renamed to <user_id>/<asset_id>.<ext> before the transaction, so that no row ever
// points to a missing file, and removed on every path that does not end with a new asset: a
// refusal, a replay, an error. A replay whose file cannot be removed still answers with the
// existing item: the failure is logged, and the orphan file is harmless.
func (s *Service) CaptureImage(ctx context.Context, userID string, req ImageRequest) (result Result, err error) {
	var assetID string
	defer func() {
		if err == nil && result.Created {
			req.Image.Keep()
			return
		}
		removeErr := req.Image.Remove()
		switch {
		case removeErr == nil:
		case err == nil:
			// Only identifiers: the error names the file.
			s.logger.WarnContext(ctx, "remove the file of a replayed image capture",
				slog.String("asset_id", assetID), slog.String("item_id", result.Item.ID))
		default:
			// The transport turns a refusal into a 4xx and drops the error: log the failure
			// here, with identifiers only. Before Commit the file is a .tmp-* removed at the
			// next start-up; after it, an orphan file the design accepts.
			attrs := []any{}
			if assetID != "" {
				attrs = append(attrs, slog.String("asset_id", assetID))
			}
			s.logger.WarnContext(ctx, "remove the file of an image capture not stored", attrs...)
			err = errors.Join(err, fmt.Errorf("capture image: %w", removeErr))
		}
	}()

	if req.Image.Size() > MaxImageSize {
		return Result{}, ErrImageTooLarge
	}
	if err := checkClientID(req.ClientID); err != nil {
		return Result{}, err
	}
	source, err := checkSource(req.Source)
	if err != nil {
		return Result{}, err
	}
	format, err := detectImage(req.Image, req.Image.Size())
	if err != nil {
		return Result{}, err
	}

	// RG-09: a blank capture note is absent.
	userNote := trim(req.UserNote)
	if userNote != nil && *userNote == "" {
		userNote = nil
	}

	assetID, err = s.store.NewID()
	if err != nil {
		return Result{}, fmt.Errorf("capture image: %w", err)
	}
	// Server identifiers only: nothing the client sent reaches the path.
	path := userID + "/" + assetID + "." + format.ext
	if err := req.Image.Commit(path); err != nil {
		return Result{}, fmt.Errorf("capture image: %w", err)
	}

	in := store.NewItem{
		Kind:      KindImage,
		Source:    source,
		Status:    StatusQueued,
		TitleAt:   func(now time.Time) string { return imageTitle(userNote, now, s.paris) },
		UserNote:  userNote,
		ClientID:  req.ClientID,
		IsPrivate: req.IsPrivate != nil && *req.IsPrivate,
		Asset: &store.NewAsset{
			ID:        assetID,
			Path:      path,
			MimeType:  format.mimeType,
			Width:     format.width,
			Height:    format.height,
			SizeBytes: req.Image.Size(),
		},
	}
	item, created, err := s.store.CaptureItem(ctx, userID, in, nil)
	if err != nil {
		return Result{}, fmt.Errorf("capture image: %w", err)
	}
	return Result{Item: item, Created: created}, nil
}

// imageFormat is a recognised image format, with the dimensions of a decoded header.
type imageFormat struct {
	mimeType string
	ext      string
	width    *int64 // nil for the formats not decoded (HEIC, WebP)
	height   *int64
}

// sniffSize is how many bytes the signatures need: a WebP header is 16 bytes long.
const sniffSize = 16

// maxFtypBox bounds the ftyp box read for the HEIC brands: real ones hold a few brands.
const maxFtypBox = 4096

var pngSignature = []byte("\x89PNG\r\n\x1a\n")

// detectImage recognises the format of the file r of size bytes from its content (RG-02),
// whatever type the client declared. JPEG and PNG headers are decoded, so that a truncated or
// forged header is refused; HEIC and WebP are recognised by their signature only, without
// dimensions (no decoder in the standard library).
func detectImage(r io.ReaderAt, size int64) (imageFormat, error) {
	head := make([]byte, sniffSize)
	n, err := r.ReadAt(head, 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return imageFormat{}, fmt.Errorf("read image header: %w", err)
	}
	head = head[:n]

	switch {
	case bytes.HasPrefix(head, []byte{0xFF, 0xD8, 0xFF}):
		return decodedFormat(r, size, "image/jpeg", "jpg", jpeg.DecodeConfig)
	case bytes.HasPrefix(head, pngSignature):
		return decodedFormat(r, size, "image/png", "png", png.DecodeConfig)
	case isWebP(head):
		return imageFormat{mimeType: "image/webp", ext: "webp"}, nil
	}
	heic, err := isHEIC(r, size)
	if err != nil {
		return imageFormat{}, err
	}
	if heic {
		return imageFormat{mimeType: "image/heic", ext: "heic"}, nil
	}
	return imageFormat{}, ErrUnsupportedImage
}

// decodedFormat decodes the header of r with decode; a header that does not decode is an
// unsupported image, not an image without dimensions. The decoder error is dropped: the
// capture is refused whatever it says.
func decodedFormat(r io.ReaderAt, size int64, mimeType, ext string, decode func(io.Reader) (image.Config, error)) (imageFormat, error) {
	config, err := decode(io.NewSectionReader(r, 0, size))
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return imageFormat{}, ErrUnsupportedImage
	}
	width, height := int64(config.Width), int64(config.Height)
	return imageFormat{mimeType: mimeType, ext: ext, width: &width, height: &height}, nil
}

// isWebP tells whether head is a RIFF WEBP header whose first chunk is a VP8, VP8L or VP8X
// image chunk.
func isWebP(head []byte) bool {
	if len(head) < 16 || string(head[0:4]) != "RIFF" || string(head[8:12]) != "WEBP" {
		return false
	}
	switch string(head[12:16]) {
	case "VP8 ", "VP8L", "VP8X":
		return true
	default:
		return false
	}
}

// isHEIC tells whether r starts with a complete ftyp box naming HEIC: major brand heic, heix,
// heim or heis, or major brand mif1 or msf1 with heic or heix among the compatible brands. An
// AVIF (mif1 with avif only) is not HEIC.
func isHEIC(r io.ReaderAt, size int64) (bool, error) {
	if size < 16 {
		return false, nil
	}
	var header [8]byte
	if _, err := r.ReadAt(header[:], 0); err != nil {
		return false, fmt.Errorf("read image header: %w", err)
	}
	if string(header[4:8]) != "ftyp" {
		return false, nil
	}
	boxSize := int64(binary.BigEndian.Uint32(header[0:4]))
	// 16 bytes at least: size, type, major brand, minor version. A box larger than the file is
	// truncated.
	if boxSize < 16 || boxSize > maxFtypBox || boxSize > size {
		return false, nil
	}
	box := make([]byte, boxSize)
	if _, err := r.ReadAt(box, 0); err != nil {
		return false, fmt.Errorf("read image header: %w", err)
	}
	switch string(box[8:12]) {
	case "heic", "heix", "heim", "heis":
		return true, nil
	case "mif1", "msf1":
		for i := 16; i+4 <= len(box); i += 4 {
			if brand := string(box[i : i+4]); brand == "heic" || brand == "heix" {
				return true, nil
			}
		}
	}
	return false, nil
}

// frenchMonths are the month names of the dated title (RG-26).
var frenchMonths = [12]string{
	"janvier", "février", "mars", "avril", "mai", "juin",
	"juillet", "août", "septembre", "octobre", "novembre", "décembre",
}

// imageTitle is the title of an image until analysis (RG-26): the first non-empty line of its
// trimmed capture note userNote, cut as a note's title, or else "Image du <day> <month>
// <year>", the day of now in loc (Paris), "1er" for the first day of a month.
func imageTitle(userNote *string, now time.Time, loc *time.Location) string {
	if userNote != nil {
		if title := noteTitle(*userNote); title != "" {
			return title
		}
	}
	day := now.In(loc)
	dayNumber := strconv.Itoa(day.Day())
	if day.Day() == 1 {
		dayNumber = "1er"
	}
	return "Image du " + dayNumber + " " + frenchMonths[day.Month()-1] + " " + strconv.Itoa(day.Year())
}
