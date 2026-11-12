package gifs

import "io"

// newLimitedReader bounds a response body.
//
// io.LimitReader stops silently at the limit, which would leave a truncated
// JSON document to fail as a parse error somewhere confusing. Reading one byte
// beyond means the decoder sees the truncation as what it is.
func newLimitedReader(r io.Reader, limit int64) io.Reader {
	return io.LimitReader(r, limit+1)
}
