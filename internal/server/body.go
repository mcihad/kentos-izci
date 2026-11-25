package server

import (
	"io"
	"net/http"
	"time"
)

// Time clients get to send a request body: a base plus the body's size at
// the slowest rate allowed. Without it a client that sends its body a byte
// at a time holds a connection (and a request slot) forever; the server
// has no overall ReadTimeout because streaming responses run for long.
const (
	bodyBaseTime = 30 * time.Second
	// bodyMinRate is the slowest upload rate allowed, bytes per second
	// (512 kbit/s).
	bodyMinRate = 64 << 10
	// bodyAssumedSize is the size assumed when the client does not declare
	// one (chunked bodies), and the most any body is granted time for: the
	// largest bodies izci takes are 34 MiB chunk uploads.
	bodyAssumedSize = 32 << 20
)

// limitBodyTime gives the client limited time to send the request body.
// The deadline is lifted as soon as the body is read, so that slow
// processing and streaming responses are not cut off.
func limitBodyTime(w http.ResponseWriter, r *http.Request, base time.Duration, rate int64) {
	if r.Body == nil || r.Body == http.NoBody {
		return
	}
	size := r.ContentLength
	if size < 0 || size > bodyAssumedSize {
		size = bodyAssumedSize
	}
	rc := http.NewResponseController(w)
	if rc.SetReadDeadline(time.Now().Add(base+time.Duration(size)*time.Second/time.Duration(rate))) != nil {
		return
	}
	r.Body = &timedBody{ReadCloser: r.Body, rc: rc}
}

// timedBody lifts the read deadline once the body is read (or failed).
type timedBody struct {
	io.ReadCloser
	rc   *http.ResponseController
	done bool
}

func (b *timedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil && !b.done {
		b.done = true
		_ = b.rc.SetReadDeadline(time.Time{})
	}
	return n, err
}
