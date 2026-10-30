// Package logtail streams a growing log file: the last lines already written,
// then whatever is appended after. It exists because some game images discard
// the server console on stdout, so the only copy is a file on disk.
package logtail

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os"
	"time"
)

const (
	pollInterval = 700 * time.Millisecond
	maxTailBytes = 256 << 10
)

// Follow returns the last tailLines of path followed by anything appended
// afterwards. The reader closes when ctx is cancelled.
func Follow(ctx context.Context, path string, tailLines int) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	start, err := seekToTail(f, tailLines)
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		_ = f.Close()
		return nil, err
	}

	pr, pw := io.Pipe()
	go func() {
		defer f.Close()
		defer pw.Close()
		reader := bufio.NewReader(f)
		for {
			if ctx.Err() != nil {
				return
			}
			n, err := io.Copy(pw, reader)
			if err != nil {
				return
			}
			if n == 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(pollInterval):
				}
			}
			reader.Reset(f)
		}
	}()
	return pr, nil
}

// seekToTail returns the offset of the start of the last tailLines lines,
// reading at most maxTailBytes so a huge file cannot be pulled into memory.
func seekToTail(f *os.File, tailLines int) (int64, error) {
	info, err := f.Stat()
	if err != nil {
		return 0, err
	}
	size := info.Size()
	if tailLines <= 0 || size == 0 {
		return size, nil
	}

	window := min(size, int64(maxTailBytes))
	buf := make([]byte, window)
	if _, err := f.ReadAt(buf, size-window); err != nil && err != io.EOF {
		return 0, err
	}

	newlines := 0
	for i := len(buf) - 1; i >= 0; i-- {
		if buf[i] != '\n' {
			continue
		}
		newlines++
		if newlines > tailLines {
			return size - window + int64(i) + 1, nil
		}
	}
	return size - window, nil
}

// LineCount reports how many lines the tail window holds, for tests.
func LineCount(b []byte) int { return bytes.Count(b, []byte{'\n'}) }
