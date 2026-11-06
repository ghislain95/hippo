// Package imagecopy makes the copy of an image sent to the AI (RG-12, RG-92): the stored image
// decoded, scaled down so that its longer side is at most LongEdge pixels (never up), turned
// upright from its orientation metadata, and encoded as a JPEG without any metadata. The stored
// file never changes.
//
// Decoding runs in a child process of the same binary, `hippo copy-image` (RunChild), bounded
// in memory and in time by internal/childproc: a decoded photo of 48 Mpx holds more than a
// hundred megabytes, and a hostile file may hold anything. The parent, Copier, hands the child
// the stored file as descriptor 3 and the output file as descriptor 4; no path crosses the
// process boundary. It is the only package that decodes images beyond their header (depguard
// rule image-decoding-only-in-imagecopy).
package imagecopy

import "time"

const (
	// ChildCommand is the subcommand of the binary that copies one image (RunChild).
	ChildCommand = "copy-image"
	// MaxPixels is the largest image copied, in pixels, read from its header before decoding:
	// it admits the 48 Mpx photos of recent iPhones (design.md D10, measures of D15).
	MaxPixels = 50_000_000
	// LongEdge is the longer side of the copy at most, in pixels.
	LongEdge = 1568
	// Quality is the JPEG quality of the copy.
	Quality = 85
	// MaxOutput bounds the copy, in bytes: beyond it, the item is sent without its image.
	MaxOutput = 5 << 20
	// ChildMemoryLimit is the memory the child's runtime may hold; above it, the child exits
	// and the item is sent without its image (too_large).
	ChildMemoryLimit = 256 << 20
	// Timeout bounds one copy, the spawn of the child included.
	Timeout = 30 * time.Second
)

// Reasons of a copy: ReasonOK, or why the item is sent without its image.
const (
	ReasonOK            = "ok"
	ReasonTooManyPixels = "too_many_pixels" // the header declares more than MaxPixels
	ReasonUndecodable   = "undecodable"     // not an image the child decodes
	ReasonTooLarge      = "too_large"       // above the memory limit, or a copy above MaxOutput
	ReasonTimeout       = "timeout"         // the child ran beyond its time limit
	ReasonCopyFailed    = "copy_failed"     // the child crashed or gave no valid answer
)

const (
	// softLimitShare is the share of the memory limit at which the garbage collector of the
	// child works harder: 200 MiB of 256 MiB.
	softLimitNum, softLimitDen = 200, 256
	// maxRequest bounds the request line the child reads.
	maxRequest = 4 << 10
	// maxAnswer bounds the answer the parent reads.
	maxAnswer = 4 << 10
	// watchInterval is how often the child samples its memory: 10 ms rather than the 1 ms of
	// childproc.Guard. Each sample stops the world for a moment; at 1 ms, a 48 Mpx HEIC took
	// 13 to 16 s in the container (linux/arm64, --memory 384m) against 2.3 s at 10 ms
	// (design.md D10, D15). A decoded image grows by large steps the ceiling already bounds.
	watchInterval = 10 * time.Millisecond
	// childWaitDelay bounds the wait for the pipes of a child killed at its time limit.
	childWaitDelay = time.Second
	// assetDescriptor and outputDescriptor are the descriptors of the stored file and of the
	// copy in the child: exec.Cmd.ExtraFiles in this order.
	assetDescriptor  = 3
	outputDescriptor = 4
	// exitOutput is the exit code of a child that could not write its copy or its answer.
	exitOutput = 3
)

// request is the line the child reads on its standard input: the limits of the parent, so that
// tests can lower them. The child refuses limits above its constants.
type request struct {
	MaxPixels   int64  `json:"max_pixels"`
	LongEdge    int    `json:"long_edge"`
	Quality     int    `json:"quality"`
	MaxOutput   int64  `json:"max_output"`
	MemoryLimit uint64 `json:"memory_limit"`
}

// valid tells whether the child accepts r.
func (r request) valid() bool {
	return r.MaxPixels > 0 && r.MaxPixels <= MaxPixels &&
		r.LongEdge > 0 && r.LongEdge <= LongEdge &&
		r.Quality > 0 && r.Quality <= 100 &&
		r.MaxOutput > 0 && r.MaxOutput <= MaxOutput &&
		r.MemoryLimit > 0 && r.MemoryLimit <= ChildMemoryLimit
}

// answer is what the child writes on its standard output.
type answer struct {
	Reason    string `json:"reason"`
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
	PeakBytes uint64 `json:"peak_bytes"`
}

// childReasons are the reasons the child gives: an answer with another one is not the child's.
var childReasons = map[string]bool{
	ReasonOK: true, ReasonTooManyPixels: true, ReasonUndecodable: true, ReasonTooLarge: true,
}
