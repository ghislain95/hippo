package childproc

import "bytes"

// CappedBuffer keeps the first bytes written up to its limit and drops the rest, so that a
// child that writes too much neither fills the memory of the service nor blocks on its pipe.
// The buffer is a field, not embedded: an embedded bytes.Buffer would lend its ReadFrom, which
// io.Copy (and exec.Cmd with it) prefers to Write, and the limit would never apply.
type CappedBuffer struct {
	buf      bytes.Buffer
	limit    int
	overflow bool
}

// NewCappedBuffer returns a buffer that keeps limit bytes at most.
func NewCappedBuffer(limit int) *CappedBuffer {
	return &CappedBuffer{limit: limit}
}

// Write keeps what fits and takes the rest without keeping it; it never fails.
func (b *CappedBuffer) Write(p []byte) (int, error) {
	if room := b.limit - b.buf.Len(); len(p) > room {
		b.overflow = true
		if room > 0 {
			b.buf.Write(p[:room])
		}
		return len(p), nil
	}
	return b.buf.Write(p)
}

// Bytes returns what was kept.
func (b *CappedBuffer) Bytes() []byte { return b.buf.Bytes() }

// Overflow tells whether more than the limit was written.
func (b *CappedBuffer) Overflow() bool { return b.overflow }
