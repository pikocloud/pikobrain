package utils

import (
	"errors"
	"io"
)

var ErrStreamTooBig = errors.New("stream is too big")

// NewLimitedReader limits maximum number of bytes to read.
// If at least one byte read on top of if - [ErrStreamTooBig] returned.
func NewLimitedReader(source io.Reader, limit int) io.Reader {
	return &limitedReader{
		wrap: source,
		left: limit,
	}
}

type limitedReader struct {
	wrap io.Reader
	left int
}

func (lr *limitedReader) Read(p []byte) (n int, err error) {
	if lr.left < 0 {
		return 0, ErrStreamTooBig
	}

	allowed := min(lr.left+1, len(p))

	x, err := lr.wrap.Read(p[:allowed])
	lr.left -= x
	if lr.left < 0 {
		return x, errors.Join(ErrStreamTooBig, err)
	}

	return x, err
}

func ReadCloser(reader io.Reader, closer io.Closer) io.ReadCloser {
	return &readCloser{
		Reader: reader,
		Closer: closer,
	}
}

type readCloser struct {
	io.Reader
	io.Closer
}
