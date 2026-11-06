package analysis

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"strconv"

	"github.com/ghislain95/hippo/backend/internal/analysis/prompt"
	"github.com/ghislain95/hippo/backend/internal/capture"
	"github.com/ghislain95/hippo/backend/internal/imagecopy"
	"github.com/ghislain95/hippo/backend/internal/store"
)

// Fixed bytes of the body (design.md D4, D6).
const (
	bodyOpen  = `{"requests":[`
	bodyClose = `]}`
	// imageOpen and imageClose surround the base64 data of an image block.
	imageOpen  = `,{"type":"image","source":{"type":"base64","media_type":"image/jpeg","data":"`
	imageClose = `"}}`
	// requestClose closes the content, the message, the messages, the params and the request.
	requestClose = `]}]}}`
	// segmentBuffer is the write buffer of a request.
	segmentBuffer = 64 << 10
)

// assetMissing is the reason of an image whose stored file cannot be opened.
const assetMissing = "asset_missing"

// RequestsConfig is what the request writer of one user's batch needs.
type RequestsConfig struct {
	UserID string
	// Model is the configured model (AnalysisModel).
	Model string
	// Prompt is the fixed part of the prompt and the classify_item tool.
	Prompt prompt.Prompt
	// Referentiel is the référentiel of the user (LoadReferentiel), the same in every request.
	Referentiel string
	// BodyLimit bounds the body, in bytes; zero or less means MaxBodySize. Tests lower it.
	BodyLimit int64
	Contents  ContentReader
	Assets    AssetOpener
	Copier    ImageCopier
	Temp      TempFiles
	// Logger receives one line per image sent without its copy; nil discards them.
	Logger *slog.Logger
}

// imageWithoutCopy is an image of the request being written that goes without its copy.
type imageWithoutCopy struct {
	assetID, reason string
}

// segment is the place of one request in the segment file.
type segment struct {
	offset, length int64
}

// Requests writes the requests of one user's batch, in order, to a segment file: an unnamed
// file of DATA_DIR/tmp, so that the service never holds a body and a crash leaves nothing
// (design.md D6). The body is then assembled from the segments of the items the submission
// moved (WriteBody). It is used by one goroutine; Close releases its files.
type Requests struct {
	cfg    RequestsConfig
	limit  int64
	shared []byte // "tools", "tool_choice" and "system": the same bytes in every request

	segW, segR   *os.File
	copyW, copyR *os.File // the image copy, created at the first image

	segments map[string]segment
	ids      []string
	end      int64              // size of the segment file
	body     int64              // size of the body of every segment written
	without  int                // images sent without their copy
	pending  []imageWithoutCopy // images without copy of the request being written
	closed   bool
}

// NewRequests creates the segment file of a batch and prepares the parts every request shares.
func NewRequests(cfg RequestsConfig) (*Requests, error) {
	switch {
	case cfg.UserID == "" || cfg.Model == "" || cfg.Referentiel == "":
		return nil, errors.New("request writer: user, model and référentiel are required")
	case cfg.Prompt.Fixed == "" || cfg.Prompt.ToolDescription == "" || len(cfg.Prompt.InputSchema) == 0:
		return nil, errors.New("request writer: the prompt is not loaded")
	case cfg.Contents == nil || cfg.Assets == nil || cfg.Copier == nil || cfg.Temp == nil:
		return nil, errors.New("request writer: contents, assets, copier and temporary files are required")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.DiscardHandler)
	}
	limit := cfg.BodyLimit
	if limit <= 0 {
		limit = MaxBodySize
	}
	shared, err := sharedParams(cfg)
	if err != nil {
		return nil, err
	}
	segW, segR, err := cfg.Temp.NewFile()
	if err != nil {
		return nil, fmt.Errorf("%w: segment file: %w", errStorage, err)
	}
	return &Requests{
		cfg: cfg, limit: limit, shared: shared, segW: segW, segR: segR,
		segments: map[string]segment{}, body: int64(len(bodyOpen) + len(bodyClose)),
	}, nil
}

// sharedParams returns `"tools":…,"tool_choice":…,"system":[…]`: the forced classify_item
// tool with the bundled schema, then the fixed part and the référentiel, a cache point after the
// référentiel (docs/prompt/README.md « Structure d'une requête »).
func sharedParams(cfg RequestsConfig) ([]byte, error) {
	type cacheControl struct {
		Type string `json:"type"`
	}
	type textBlock struct {
		Type         string        `json:"type"`
		Text         string        `json:"text"`
		CacheControl *cacheControl `json:"cache_control,omitempty"`
	}
	type tool struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"input_schema"`
	}
	raw, err := marshal(struct {
		Tools      []tool `json:"tools"`
		ToolChoice struct {
			Type string `json:"type"`
			Name string `json:"name"`
		} `json:"tool_choice"`
		System []textBlock `json:"system"`
	}{
		Tools: []tool{{Name: toolName, Description: cfg.Prompt.ToolDescription, InputSchema: cfg.Prompt.InputSchema}},
		ToolChoice: struct {
			Type string `json:"type"`
			Name string `json:"name"`
		}{Type: "tool", Name: toolName},
		System: []textBlock{
			{Type: "text", Text: cfg.Prompt.Fixed},
			{Type: "text", Text: cfg.Referentiel, CacheControl: &cacheControl{Type: "ephemeral"}},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("request writer: %w", err)
	}
	return raw[1 : len(raw)-1], nil // without the braces of the object
}

// marshal encodes v without escaping <, > and &: <element> stays readable, and shorter.
func marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// Add appends the request of item to the segment file. added is false, with a nil error, when
// the request would take the body beyond its limit: nothing of it is kept, and the selection
// stops there, the item and the later ones waiting for the next pass (RG-32). An error stops the
// user's batch, except ErrItemGone (the item is skipped):
//
//   - ErrPrivateItem for a private item, before anything is read or written (RG-12);
//   - ErrCopierUnavailable when the image copier cannot start;
//   - a storage error when a temporary file cannot be written;
//   - the error of ctx when it is done.
//
// An image that cannot be copied is sent without its image block, logged with its reason.
func (r *Requests) Add(ctx context.Context, item store.AnalysisQueueItem) (added bool, err error) {
	if r.closed {
		return false, errors.New("request writer: closed")
	}
	if item.IsPrivate {
		// Checked again rather than trusted from the selection: fail closed.
		return false, fmt.Errorf("request of item %s: %w", item.ID, ErrPrivateItem)
	}
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("request of item %s: %w", item.ID, err)
	}
	var content *string
	if item.Kind == capture.KindLink {
		content, err = r.cfg.Contents.ItemContent(ctx, r.cfg.UserID, item.ID)
		if errors.Is(err, store.ErrNotFound) {
			return false, fmt.Errorf("request of item %s: %w", item.ID, ErrItemGone)
		}
		if err != nil {
			return false, fmt.Errorf("request of item %s: %w", item.ID, err)
		}
	}

	separator := int64(0)
	if len(r.ids) > 0 {
		separator = 1 // the comma before this segment in the body
	}
	room := r.limit - r.body - separator
	out := &limitedWriter{w: io.NewOffsetWriter(r.segW, r.end), room: room}
	buf := bufio.NewWriterSize(out, segmentBuffer)
	r.pending = r.pending[:0]
	length, err := r.write(ctx, buf, item, content)
	if err == nil {
		err = buf.Flush()
	}
	if err != nil {
		// Nothing of a request that does not fit, nor of a failed one, stays in the file.
		if truncErr := r.segW.Truncate(r.end); truncErr != nil {
			return false, fmt.Errorf("%w: truncate: %w", errStorage, pathless(truncErr))
		}
		if errors.Is(err, errBodyFull) {
			return false, nil
		}
		return false, fmt.Errorf("request of item %s: %w", item.ID, err)
	}
	r.segments[item.ID] = segment{offset: r.end, length: length}
	r.ids = append(r.ids, item.ID)
	r.end += length
	r.body += separator + length
	// Only an image whose request is kept is logged and counted as sent without its copy.
	for _, p := range r.pending {
		r.without++
		r.cfg.Logger.Warn("image sent without its copy",
			slog.String("user_id", r.cfg.UserID), slog.String("item_id", item.ID),
			slog.String("asset_id", p.assetID), slog.String("reason", p.reason))
	}
	r.pending = r.pending[:0]
	return true, nil
}

// write writes the request of item to w and returns its length.
func (r *Requests) write(ctx context.Context, w *bufio.Writer, item store.AnalysisQueueItem, content *string) (int64, error) {
	text, err := elementText(item, content)
	if err != nil {
		return 0, err
	}
	id, err := marshal(item.ID)
	if err != nil {
		return 0, err
	}
	model, err := marshal(r.cfg.Model)
	if err != nil {
		return 0, err
	}
	textBlock, err := marshal(struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}{Type: "text", Text: text})
	if err != nil {
		return 0, err
	}
	capTokens := maxTokens
	if item.IncompleteRetried {
		capTokens = maxTokensRetried
	}

	counter := &countingWriter{w: w}
	for _, part := range [][]byte{
		[]byte(`{"custom_id":`), id, []byte(`,"params":{"model":`), model,
		[]byte(`,"max_tokens":` + strconv.Itoa(capTokens) + `,`), r.shared,
		[]byte(`,"messages":[{"role":"user","content":[`), textBlock,
	} {
		if _, err := counter.Write(part); err != nil {
			return 0, err
		}
	}
	if item.Kind == capture.KindImage {
		for _, asset := range item.Assets {
			if err := r.writeImage(ctx, counter, asset); err != nil {
				return 0, err
			}
		}
	}
	if _, err := counter.Write([]byte(requestClose)); err != nil {
		return 0, err
	}
	return counter.n, nil
}

// writeImage copies the stored image of asset and writes its image block to w, the base64 data
// streamed from the copy, never held whole. An image that cannot be copied writes nothing and
// is recorded with its reason (withoutCopy); only a copier that cannot start, a done context or a temporary file
// that fails return an error.
func (r *Requests) writeImage(ctx context.Context, w io.Writer, asset store.Asset) error {
	if r.copyW == nil {
		copyW, copyR, err := r.cfg.Temp.NewFile()
		if err != nil {
			return fmt.Errorf("%w: image copy file: %w", errStorage, err)
		}
		r.copyW, r.copyR = copyW, copyR
	}
	stored, ok := r.openAsset(asset.Path)
	if !ok {
		// The file is missing or unreadable: the item goes without its image, the batch goes on.
		r.withoutCopy(asset.ID, assetMissing)
		return nil
	}
	res, err := r.cfg.Copier.Copy(ctx, stored, r.copyW)
	_ = stored.Close() // read-only
	switch {
	case errors.Is(err, imagecopy.ErrUnavailable):
		return fmt.Errorf("%w: %w", ErrCopierUnavailable, err)
	case errors.Is(err, imagecopy.ErrStopped):
		return fmt.Errorf("image copy: %w", errors.Join(err, ctx.Err()))
	case err != nil:
		return fmt.Errorf("%w: image copy: %w", errStorage, err)
	case res.Reason != imagecopy.ReasonOK:
		r.withoutCopy(asset.ID, res.Reason)
		return nil
	}

	if _, err := io.WriteString(w, imageOpen); err != nil {
		return err
	}
	enc := base64.NewEncoder(base64.StdEncoding, w)
	if _, err := io.Copy(enc, io.NewSectionReader(r.copyR, 0, res.Size)); err != nil {
		return fmt.Errorf("image copy: %w", wrapRead(err))
	}
	if err := enc.Close(); err != nil {
		return err
	}
	_, err = io.WriteString(w, imageClose)
	return err
}

// openAsset opens the stored image rel; ok is false when it cannot, whatever the cause, which
// is not logged: the error of the file names its path.
func (r *Requests) openAsset(rel string) (*os.File, bool) {
	file, err := r.cfg.Assets.Open(rel)
	if err != nil {
		return nil, false
	}
	return file, true
}

// withoutCopy records an image of the request being written that goes without its copy. Add
// logs it (ids and reason only, RG-92) and counts it once the request is kept: a request that
// does not fit is not sent, so neither is logged.
func (r *Requests) withoutCopy(assetID, reason string) {
	r.pending = append(r.pending, imageWithoutCopy{assetID: assetID, reason: reason})
}

// IDs returns the ids of the requests written, in order.
func (r *Requests) IDs() []string {
	return append([]string(nil), r.ids...)
}

// ImagesWithoutCopy returns the number of images sent without their copy so far.
func (r *Requests) ImagesWithoutCopy() int {
	return r.without
}

// WriteBody writes to dst the body of the Message Batches create call holding the requests of
// ids, in this order, each copied from its segment: `{"requests":[…]}`. It returns the size of
// the body. An id without a request is an error: the body holds only what was written.
func (r *Requests) WriteBody(dst io.Writer, ids []string) (int64, error) {
	if r.closed {
		return 0, errors.New("request writer: closed")
	}
	counter := &countingWriter{w: dst}
	if _, err := io.WriteString(counter, bodyOpen); err != nil {
		return counter.n, fmt.Errorf("write body: %w", err)
	}
	for i, id := range ids {
		seg, ok := r.segments[id]
		if !ok {
			return counter.n, fmt.Errorf("write body: no request for item %s", id)
		}
		if i > 0 {
			if _, err := io.WriteString(counter, ","); err != nil {
				return counter.n, fmt.Errorf("write body: %w", err)
			}
		}
		if _, err := io.Copy(counter, io.NewSectionReader(r.segR, seg.offset, seg.length)); err != nil {
			return counter.n, fmt.Errorf("write body: %w", wrapRead(err))
		}
	}
	if _, err := io.WriteString(counter, bodyClose); err != nil {
		return counter.n, fmt.Errorf("write body: %w", err)
	}
	return counter.n, nil
}

// Close closes the temporary files; their data goes with them. It may run more than once.
func (r *Requests) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	var errs []error
	for _, f := range []*os.File{r.segW, r.segR, r.copyW, r.copyR} {
		if f != nil {
			if err := f.Close(); err != nil {
				errs = append(errs, pathless(err))
			}
		}
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("close request writer: %w", err)
	}
	return nil
}

// limitedWriter writes to w at most room bytes, then fails with errBodyFull; a write error of w
// is a storage error.
type limitedWriter struct {
	w    io.Writer
	room int64
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > l.room {
		return 0, errBodyFull
	}
	n, err := l.w.Write(p)
	l.room -= int64(n)
	if err != nil {
		return n, fmt.Errorf("%w: write: %w", errStorage, pathless(err))
	}
	return n, nil
}

// countingWriter counts the bytes written to w.
type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// wrapRead marks a read error of a temporary file as a storage error, naming no file.
func wrapRead(err error) error {
	if errors.Is(err, errStorage) || errors.Is(err, errBodyFull) {
		return err
	}
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		return fmt.Errorf("%w: read: %w", errStorage, pathless(err))
	}
	return err
}

// pathless drops the path of a file error.
func pathless(err error) error {
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		return fmt.Errorf("%s: %w", pathErr.Op, pathErr.Err)
	}
	return err
}
