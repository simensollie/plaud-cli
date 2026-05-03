package api

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

// blockingReader blocks on Read until Close is called or until a value is
// pushed to the feed channel. It is the test substitute for a TCP-wedged
// connection.
type blockingReader struct {
	mu     sync.Mutex
	closed bool
	feed   chan []byte
	done   chan struct{}
}

func newBlockingReader() *blockingReader {
	return &blockingReader{
		feed: make(chan []byte, 16),
		done: make(chan struct{}),
	}
}

func (r *blockingReader) Read(p []byte) (int, error) {
	select {
	case b, ok := <-r.feed:
		if !ok {
			return 0, io.EOF
		}
		n := copy(p, b)
		return n, nil
	case <-r.done:
		return 0, io.ErrClosedPipe
	}
}

func (r *blockingReader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	close(r.done)
	return nil
}

func TestIdleReader_F15_ProgressResetsTimer(t *testing.T) {
	br := newBlockingReader()
	idle := newIdleTimeoutReader(br, 80*time.Millisecond)
	t.Cleanup(func() { _ = idle.Close() })

	// Push four bursts at 30ms intervals; each well under the 80ms timeout.
	go func() {
		for i := 0; i < 4; i++ {
			time.Sleep(30 * time.Millisecond)
			br.feed <- []byte("abc")
		}
		close(br.feed)
	}()

	var buf bytes.Buffer
	n, err := io.Copy(&buf, idle)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("io.Copy returned %v, want nil or EOF", err)
	}
	if n != 12 {
		t.Errorf("bytes read = %d, want 12", n)
	}
	if got := buf.String(); got != "abcabcabcabc" {
		t.Errorf("buffer = %q, want %q", got, "abcabcabcabc")
	}
}

func TestIdleReader_F15_StallReturnsErrIdleTimeout(t *testing.T) {
	br := newBlockingReader()
	idle := newIdleTimeoutReader(br, 50*time.Millisecond)
	t.Cleanup(func() { _ = idle.Close() })

	buf := make([]byte, 16)
	_, err := idle.Read(buf)
	if !errors.Is(err, ErrIdleTimeout) {
		t.Fatalf("Read err = %v, want ErrIdleTimeout", err)
	}
}

func TestIdleReader_F15_CloseIsIdempotent(t *testing.T) {
	br := newBlockingReader()
	idle := newIdleTimeoutReader(br, 1*time.Second)
	if err := idle.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := idle.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestIdleReader_F15_NormalEOFPropagated(t *testing.T) {
	src := io.NopCloser(bytes.NewReader([]byte("hello plaud")))
	idle := newIdleTimeoutReader(src, 1*time.Second)
	t.Cleanup(func() { _ = idle.Close() })

	var out bytes.Buffer
	n, err := io.Copy(&out, idle)
	if err != nil {
		t.Fatalf("io.Copy: %v", err)
	}
	if n != int64(len("hello plaud")) {
		t.Errorf("n = %d, want %d", n, len("hello plaud"))
	}
	if errors.Is(err, ErrIdleTimeout) {
		t.Errorf("got ErrIdleTimeout for natural EOF")
	}
}
