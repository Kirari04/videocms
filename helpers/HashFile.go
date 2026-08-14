package helpers

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
)

func HashFile(file string) (string, error) {
	return HashFileContext(context.Background(), file)
}

func HashFileContext(ctx context.Context, file string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	f, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer f.Close()

	// obtain hash from file
	h := sha256.New()
	if _, err := io.Copy(h, contextReader{ctx: ctx, reader: f}); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(buffer []byte) (int, error) {
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	default:
		return r.reader.Read(buffer)
	}
}
