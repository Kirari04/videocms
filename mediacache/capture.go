package mediacache

import (
	"errors"
	"io"
	"os"
	"sync"

	"ch/kirari04/videocms/storage"
)

type captureBody struct {
	source   storage.ReadSeekCloser
	target   *os.File
	expected int64
	position int64
	written  int64
	started  bool
	valid    bool
	done     func(bool)
	once     sync.Once
	closeErr error
}

func newCaptureBody(source storage.ReadSeekCloser, target *os.File, expected int64, done func(bool)) *captureBody {
	return &captureBody{source: source, target: target, expected: expected, valid: true, done: done}
}

func (r *captureBody) Read(buffer []byte) (int, error) {
	count, readErr := r.source.Read(buffer)
	if count > 0 {
		if !r.started {
			r.started = true
			if r.position != 0 {
				r.valid = false
			}
		}
		if r.valid && r.position == r.written {
			written, writeErr := r.target.Write(buffer[:count])
			r.written += int64(written)
			if writeErr != nil || written != count {
				r.valid = false
			}
		} else {
			r.valid = false
		}
		r.position += int64(count)
	}
	return count, readErr
}

func (r *captureBody) Seek(offset int64, whence int) (int64, error) {
	position, err := r.source.Seek(offset, whence)
	if err != nil {
		return position, err
	}
	r.position = position
	if r.started && position != r.written {
		r.valid = false
	}
	return position, nil
}

func (r *captureBody) Close() error {
	r.once.Do(func() {
		r.closeErr = errors.Join(r.source.Close(), r.target.Close())
		complete := r.closeErr == nil && r.valid && r.started && r.written == r.expected
		if r.done != nil {
			r.done(complete)
		}
	})
	return r.closeErr
}

var _ io.ReadSeekCloser = (*captureBody)(nil)
