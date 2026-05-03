package api

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const fakeAudioBody = "hello plaud"

// fakeAudioMD5 is the MD5 of fakeAudioBody, formatted as S3 would return it
// (lowercase 32 hex with surrounding quotes).
var fakeAudioMD5 = func() string {
	h := md5.Sum([]byte(fakeAudioBody))
	return hex.EncodeToString(h[:])
}()

func TestAudio_F07_ProbeReturnsETagAndTotalSize(t *testing.T) {
	const wantSize int64 = 4465808
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("probe audio carried Authorization header: %q", r.Header.Get("Authorization"))
		}
		if r.Method != http.MethodGet {
			http.Error(w, "expected GET", http.StatusBadRequest)
			return
		}
		if got := r.Header.Get("Range"); got != "bytes=0-0" {
			http.Error(w, fmt.Sprintf("expected Range bytes=0-0, got %q", got), http.StatusBadRequest)
			return
		}
		w.Header().Set("ETag", `"9c0d80abcdef9c0d80abcdef9c0d80ab"`)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-0/%d", wantSize))
		w.Header().Set("Content-Length", "1")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte{0x00})
	}))
	t.Cleanup(srv.Close)

	c, err := New(RegionEU, "tok", WithBaseURL("http://unused"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	probe, err := c.ProbeAudio(context.Background(), srv.URL+"/audiofiles/foo.mp3")
	if err != nil {
		t.Fatalf("ProbeAudio: %v", err)
	}
	if probe.ETag != "9c0d80abcdef9c0d80abcdef9c0d80ab" {
		t.Errorf("ETag = %q, want unquoted hex", probe.ETag)
	}
	if probe.SizeBytes != wantSize {
		t.Errorf("SizeBytes = %d, want %d (total from Content-Range, not chunk)", probe.SizeBytes, wantSize)
	}
}

func TestAudio_F01_StreamsBytesAndComputesLocalMD5(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"`+fakeAudioMD5+`"`)
		w.Header().Set("Content-Type", "binary/octet-stream")
		w.Header().Set("Content-Length", strconv.Itoa(len(fakeAudioBody)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(fakeAudioBody))
	}))
	t.Cleanup(srv.Close)

	c, err := New(RegionEU, "tok", WithBaseURL("http://unused"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var buf bytes.Buffer
	n, etag, localMD5, err := c.DownloadAudio(context.Background(), srv.URL+"/audiofiles/foo.mp3", &buf)
	if err != nil {
		t.Fatalf("DownloadAudio: %v", err)
	}
	if n != int64(len(fakeAudioBody)) {
		t.Errorf("n = %d, want %d", n, len(fakeAudioBody))
	}
	if buf.String() != fakeAudioBody {
		t.Errorf("written body = %q, want %q", buf.String(), fakeAudioBody)
	}
	if etag != fakeAudioMD5 {
		t.Errorf("etag = %q, want %q", etag, fakeAudioMD5)
	}
	if localMD5 != fakeAudioMD5 {
		t.Errorf("localMD5 = %q, want %q", localMD5, fakeAudioMD5)
	}
}

func TestAudio_F01_LocalMD5EqualsETagOnSinglePartUpload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"`+fakeAudioMD5+`"`)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(fakeAudioBody))
	}))
	t.Cleanup(srv.Close)

	c, err := New(RegionEU, "tok", WithBaseURL("http://unused"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var buf bytes.Buffer
	_, etag, localMD5, err := c.DownloadAudio(context.Background(), srv.URL+"/audio.mp3", &buf)
	if err != nil {
		t.Fatalf("DownloadAudio: %v", err)
	}
	if etag != localMD5 {
		t.Errorf("etag %q != localMD5 %q", etag, localMD5)
	}
}

func TestAudio_F13_DoesNotSendAuthorizationToS3(t *testing.T) {
	var sawAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			sawAuth = true
			t.Errorf("S3 leg saw Authorization header: %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("ETag", `"`+fakeAudioMD5+`"`)
		if r.Method == http.MethodGet && r.Header.Get("Range") == "bytes=0-0" {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-0/%d", len(fakeAudioBody)))
			w.Header().Set("Content-Length", "1")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte{fakeAudioBody[0]})
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(fakeAudioBody)))
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(fakeAudioBody))
			return
		}
		http.Error(w, "unexpected method", http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)

	c, err := New(RegionEU, "very-secret-bearer", WithBaseURL("http://unused"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := c.ProbeAudio(context.Background(), srv.URL+"/audio.mp3"); err != nil {
		t.Fatalf("ProbeAudio: %v", err)
	}
	var buf bytes.Buffer
	if _, _, _, err := c.DownloadAudio(context.Background(), srv.URL+"/audio.mp3", &buf); err != nil {
		t.Fatalf("DownloadAudio: %v", err)
	}
	if sawAuth {
		t.Fatal("Authorization leaked to S3 leg")
	}
}

// stallingHandler writes a prefix of bytes, flushes, then holds the
// connection open without writing anything else. It is the test substitute
// for a TCP-stalled S3 download.
type stallingHandler struct {
	prefix []byte
	hold   chan struct{}
	mu     sync.Mutex
}

func (h *stallingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, _ := w.(http.Flusher)
	w.Header().Set("ETag", `"`+fakeAudioMD5+`"`)
	w.Header().Set("Content-Length", "1000000")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(h.prefix)
	if flusher != nil {
		flusher.Flush()
	}
	<-h.hold
}

func TestAudio_F15_IdleTimeoutAbortsStalledRead(t *testing.T) {
	h := &stallingHandler{
		prefix: bytes.Repeat([]byte{0xAB}, 100),
		hold:   make(chan struct{}),
	}
	srv := httptest.NewServer(h)
	t.Cleanup(func() {
		close(h.hold)
		srv.Close()
	})

	c, err := New(RegionEU, "tok", WithBaseURL("http://unused"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var buf bytes.Buffer
	_, _, _, err = c.DownloadAudio(context.Background(), srv.URL+"/audio.mp3", &buf, withIdleTimeout(100*time.Millisecond))
	if err == nil {
		t.Fatal("DownloadAudio against stalled stream returned nil error")
	}
	if !errors.Is(err, ErrIdleTimeout) {
		t.Errorf("err = %v, want errors.Is ErrIdleTimeout", err)
	}
}

func TestAudio_F15_403ReturnsSignedURLExpired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprint(w, `<Error><Code>AccessDenied</Code></Error>`)
	}))
	t.Cleanup(srv.Close)

	c, err := New(RegionEU, "tok", WithBaseURL("http://unused"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var buf bytes.Buffer
	_, _, _, err = c.DownloadAudio(context.Background(), srv.URL+"/audio.mp3", &buf)
	if err == nil {
		t.Fatal("DownloadAudio against 403 returned nil error")
	}
	if !errors.Is(err, ErrSignedURLExpired) {
		t.Errorf("err = %v, want errors.Is ErrSignedURLExpired", err)
	}
}

func TestAudio_F15_401ReturnsSignedURLExpired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	c, err := New(RegionEU, "tok", WithBaseURL("http://unused"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var buf bytes.Buffer
	_, _, _, err = c.DownloadAudio(context.Background(), srv.URL+"/audio.mp3", &buf)
	if err == nil {
		t.Fatal("DownloadAudio against 401 returned nil error")
	}
	if !errors.Is(err, ErrSignedURLExpired) {
		t.Errorf("err = %v, want errors.Is ErrSignedURLExpired", err)
	}
}

func TestAudio_F15_404ReturnsWrappedError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	c, err := New(RegionEU, "tok", WithBaseURL("http://unused"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var buf bytes.Buffer
	_, _, _, err = c.DownloadAudio(context.Background(), srv.URL+"/audio.mp3", &buf)
	if err == nil {
		t.Fatal("DownloadAudio against 404 returned nil error")
	}
	if errors.Is(err, ErrSignedURLExpired) {
		t.Errorf("404 must NOT classify as ErrSignedURLExpired (caller would refetch URL needlessly)")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("err = %v, want message containing %q", err, "404")
	}
}

func TestAudio_F15_500ReturnsWrappedError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	c, err := New(RegionEU, "tok", WithBaseURL("http://unused"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var buf bytes.Buffer
	_, _, _, err = c.DownloadAudio(context.Background(), srv.URL+"/audio.mp3", &buf)
	if err == nil {
		t.Fatal("DownloadAudio against 500 returned nil error")
	}
	if errors.Is(err, ErrSignedURLExpired) {
		t.Errorf("500 must NOT classify as ErrSignedURLExpired")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("err = %v, want message containing %q", err, "500")
	}
}
