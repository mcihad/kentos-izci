package server

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestBodyTimeLimit checks that a client sending its body too slowly is
// cut off, and that the limit ends once the body is read.
func TestBodyTimeLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limitBodyTime(w, r, 200*time.Millisecond, 1<<30)
		if _, err := io.ReadAll(r.Body); err != nil {
			w.WriteHeader(http.StatusRequestTimeout)
			return
		}
		// Slow processing after the body is read keeps its request.
		time.Sleep(500 * time.Millisecond)
		if r.Context().Err() != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	send := func(body string, slow bool) int {
		t.Helper()
		conn, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		head := "POST / HTTP/1.1\r\nHost: izci\r\nContent-Length: 10\r\n\r\n"
		if slow {
			_, _ = io.WriteString(conn, head+body[:2])
			time.Sleep(600 * time.Millisecond)
			_, _ = io.WriteString(conn, body[2:])
		} else {
			_, _ = io.WriteString(conn, head+body)
		}
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		res, err := http.ReadResponse(bufio.NewReader(conn), nil)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if got := send("0123456789", true); got != http.StatusRequestTimeout {
		t.Fatalf("slow body: %d", got)
	}
	if got := send("0123456789", false); got != http.StatusNoContent {
		t.Fatalf("slow processing after the body: %d", got)
	}
}

// TestBodyTimeLimitStreaming checks that a streaming response whose
// handler never reads its request body to the end outlives the limit.
func TestBodyTimeLimitStreaming(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limitBodyTime(w, r, 100*time.Millisecond, 1<<30)
		buf := make([]byte, 2)
		if _, err := io.ReadFull(r.Body, buf); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
		for i := range 6 {
			time.Sleep(100 * time.Millisecond)
			if r.Context().Err() != nil {
				return
			}
			_, _ = io.WriteString(w, strings.Repeat("x", i+1)+"\n")
			http.NewResponseController(w).Flush()
		}
		_, _ = io.WriteString(w, "end\n")
	}))
	defer srv.Close()
	res, err := http.Post(srv.URL, "application/octet-stream", strings.NewReader("0123456789"))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	out, err := io.ReadAll(res.Body)
	if err != nil || !strings.HasSuffix(string(out), "end\n") {
		t.Fatalf("stream: %q %v", out, err)
	}
}
