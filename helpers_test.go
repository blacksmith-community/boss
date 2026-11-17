package main

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// seen is one request the fake broker received.
type seen struct {
	Method     string
	Path       string // path and query, as sent
	Body       string
	Auth       string
	CommandKey string
}

// reply is what a fake broker route answers.
type reply struct {
	Status      int
	File        string // a fixture under testdata/
	Body        string // used when File is empty
	ContentType string
}

// fakeBroker serves recorded broker responses and notes each request.
type fakeBroker struct {
	t      *testing.T
	srv    *httptest.Server
	mu     sync.Mutex
	routes map[string]func(n int) reply
	calls  map[string]int
	seen   []seen
}

// fixture reads a recorded response from testdata/.
func fixture(t *testing.T, name string) string {
	t.Helper()

	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return string(b)
}

// newBroker starts a fake broker. A route key is "METHOD /path" with no
// query, and its value is the fixture file to send with status 200.
func newBroker(t *testing.T, files map[string]string) *fakeBroker {
	t.Helper()

	b := &fakeBroker{t: t, routes: map[string]func(int) reply{}, calls: map[string]int{}}
	for k, f := range files {
		f := f
		b.routes[k] = func(int) reply { return reply{Status: 200, File: f} }
	}
	b.srv = httptest.NewServer(http.HandlerFunc(b.serve))
	t.Cleanup(b.srv.Close)
	return b
}

// route sets the answer for one route, replacing any earlier one.
func (b *fakeBroker) route(key string, fn func(n int) reply) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.routes[key] = fn
}

// status makes a route answer a fixture with a given status.
func (b *fakeBroker) status(key string, status int, file string) {
	b.route(key, func(int) reply { return reply{Status: status, File: file} })
}

func (b *fakeBroker) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)

	b.mu.Lock()
	b.seen = append(b.seen, seen{
		Method: r.Method, Path: r.URL.RequestURI(), Body: string(body),
		Auth: r.Header.Get("Authorization"), CommandKey: r.Header.Get(commandKeyHeader),
	})
	key := r.Method + " " + r.URL.Path
	fn, ok := b.routes[key]
	b.calls[key]++
	n := b.calls[key]
	b.mu.Unlock()

	if !ok {
		http.Error(w, `{"success":false,"error":"endpoint not found"}`, http.StatusNotFound)
		return
	}

	rep := fn(n)
	text := rep.Body
	if rep.File != "" {
		text = fixture(b.t, rep.File)
	}
	ct := rep.ContentType
	if ct == "" {
		ct = "application/json"
	}
	w.Header().Set("Content-Type", ct)
	w.WriteHeader(rep.Status)
	_, _ = io.WriteString(w, text)
}

// requests returns what the broker saw so far.
func (b *fakeBroker) requests() []seen {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]seen(nil), b.seen...)
}

// client gives a Client aimed at the fake broker.
func (b *fakeBroker) client() *Client {
	return &Client{URL: b.srv.URL, Username: "fixture-user", Password: "fixture-pass", MaxRetries: -1}
}

// testRunner builds a runner that writes to buffers and answers the
// confirmation prompt from input.
func testRunner(c *Client, input string, yes bool) (*runner, *bytes.Buffer, *bytes.Buffer) {
	out, errb := &bytes.Buffer{}, &bytes.Buffer{}
	r := &runner{
		c:    c,
		ctx:  context.Background(),
		in:   bufio.NewReader(strings.NewReader(input)),
		out:  out,
		err:  errb,
		yes:  yes,
		poll: 5 * time.Millisecond,
	}
	return r, out, errb
}

// only returns the requests that are not part of looking an instance up.
func only(reqs []seen, method string) []seen {
	var out []seen
	for _, r := range reqs {
		if r.Method == method {
			out = append(out, r)
		}
	}
	return out
}
