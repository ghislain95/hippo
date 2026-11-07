package anthropic

// BufferCap is the capacity of the line buffer of r (tests only): it proves one line at most is
// held.
func BufferCap(r *ResultReader) int { return cap(r.line) }
