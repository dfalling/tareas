package web

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// oversized echoes back how many body bytes it managed to read, or 413 when
// the read fails — what limitBody is supposed to cause.
func oversized(w http.ResponseWriter, r *http.Request) {
	n, err := io.Copy(io.Discard, r.Body)
	if err != nil {
		http.Error(w, "body read failed", http.StatusRequestEntityTooLarge)
		return
	}
	_, _ = io.WriteString(w, strings.Repeat("x", int(n)))
}

func TestLimitBodyAllowsSmallBodies(t *testing.T) {
	t.Parallel()

	h := limitBody(http.HandlerFunc(oversized))
	req := httptest.NewRequest(http.MethodPost, "/todos", strings.NewReader("title=hi&csrf_token=x"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("small body status = %d, want %d", rec.Code, http.StatusOK)
	}
	if int64(rec.Body.Len()) != req.ContentLength {
		t.Errorf("small body read %d bytes, want %d", rec.Body.Len(), req.ContentLength)
	}
}

func TestLimitBodyRejectsDeclaredOversize(t *testing.T) {
	t.Parallel()

	// A declared Content-Length over the cap is rejected before any read.
	h := limitBody(http.HandlerFunc(oversized))
	body := bytes.NewReader(make([]byte, maxBodyBytes+1))
	req := httptest.NewRequest(http.MethodPost, "/todos", body)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized body status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestLimitBodyCapsUndeclaredOversize(t *testing.T) {
	t.Parallel()

	// A chunked body (Content-Length unknown, -1) must also stop at the cap.
	h := limitBody(http.HandlerFunc(oversized))
	req := httptest.NewRequest(http.MethodPost, "/todos", bytes.NewReader(make([]byte, maxBodyBytes+1)))
	req.ContentLength = -1
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("undeclared oversize status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
}
