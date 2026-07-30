package download

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

type Assembler interface {
	Assemble(ctx context.Context, selection *Selection, outputPath string, progress func(float64)) error
}

type FFmpegAssembler struct{}

type boundedBuffer struct {
	mu    sync.Mutex
	data  []byte
	limit int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, p...)
	if len(b.data) > b.limit {
		b.data = append([]byte(nil), b.data[len(b.data)-b.limit:]...)
	}
	return len(p), nil
}

func (b *boundedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(bytes.TrimSpace(b.data))
}

func (FFmpegAssembler) Assemble(
	ctx context.Context,
	selection *Selection,
	outputPath string,
	progress func(float64),
) error {
	cmd := exec.CommandContext(ctx, "ffmpeg", FFmpegArgs(selection, outputPath, true)...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("open ffmpeg progress pipe: %w", err)
	}
	stderr := &boundedBuffer{limit: 16 * 1024}
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start ffmpeg: %w", err)
	}

	progressErr := make(chan error, 1)
	go func() {
		progressErr <- scanProgress(stdout, selection.MediaDuration, progress)
	}()

	waitErr := cmd.Wait()
	scanErr := <-progressErr
	if waitErr != nil {
		detail := stderr.String()
		if detail == "" {
			detail = waitErr.Error()
		}
		return fmt.Errorf("ffmpeg failed: %s", detail)
	}
	if scanErr != nil {
		return scanErr
	}
	if progress != nil {
		progress(1)
	}
	return nil
}

func scanProgress(reader io.Reader, duration float64, progress func(float64)) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "progress=end" {
			if progress != nil {
				progress(1)
			}
			continue
		}
		if duration <= 0 || !strings.HasPrefix(line, "out_time_ms=") {
			continue
		}
		raw := strings.TrimPrefix(line, "out_time_ms=")
		microseconds, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			continue
		}
		value := microseconds / 1_000_000 / duration
		if value < 0 {
			value = 0
		}
		if value > 0.99 {
			value = 0.99
		}
		if progress != nil {
			progress(value)
		}
	}
	return scanner.Err()
}
