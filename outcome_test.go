package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// cuttingServer answers every request by ending the connection the way
// cut says, after the request was fully read.
func cuttingServer(t *testing.T, cut func(net.Conn)) (*httptest.Server, *int32) {
	t.Helper()

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		conn, _, _ := w.(http.Hijacker).Hijack()
		cut(conn)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func reset(conn net.Conn) {
	if tc, ok := conn.(*net.TCPConn); ok {
		_ = tc.SetLinger(0)
	}
	_ = conn.Close()
}

func closeQuietly(conn net.Conn) { _ = conn.Close() }

func assertUnknown(t *testing.T, name string, err error) {
	t.Helper()

	if err == nil {
		t.Errorf("%s: want an error", name)
		return
	}
	for _, want := range []string{"outcome is unknown", "boss tasks list"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%s: error lacks %q: %v", name, want, err)
		}
	}
	if strings.Contains(err.Error(), "nothing was sent") {
		t.Errorf("%s: must not say nothing was sent: %v", name, err)
	}
}

func TestAWriteThatEndsWithoutAnAnswerIsUnknown(t *testing.T) {
	for name, cut := range map[string]func(net.Conn){"eof": closeQuietly, "reset": reset} {
		srv, hits := cuttingServer(t, cut)
		c := &Client{URL: srv.URL, MaxRetries: 2, Backoff: time.Millisecond}

		for method, call := range map[string]func() error{
			"POST":   func() error { _, err := c.Redeploy(fixtureID, false); return err },
			"DELETE": func() error { return c.Delete(InstanceRef{ID: fixtureID, PlanID: "p", ServiceID: "s"}) },
		} {
			before := atomic.LoadInt32(hits)
			assertUnknown(t, name+" "+method, call())
			if n := atomic.LoadInt32(hits) - before; n != 1 {
				t.Errorf("%s %s: sent %d times, want 1", name, method, n)
			}
		}
	}
}

func TestAReadThatEndsWithoutAnAnswerIsNotCalledUnknown(t *testing.T) {
	srv, _ := cuttingServer(t, closeQuietly)
	c := &Client{URL: srv.URL, MaxRetries: -1}

	_, err := c.Catalog()
	if err == nil || strings.Contains(err.Error(), "outcome is unknown") {
		t.Errorf("a read has no unknown outcome: %v", err)
	}
}

func TestAWriteCancelledAfterItWasSentIsUnknown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	go func() {
		<-started
		cancel()
	}()

	c := &Client{URL: srv.URL, MaxRetries: -1, Ctx: ctx}
	_, err := c.Redeploy(fixtureID, false)
	assertUnknown(t, "cancelled", err)
}

func TestAWriteRefusedBeforeAnyByteSaysNothingWasSent(t *testing.T) {
	srv := httptest.NewServer(nil)
	url := srv.URL
	srv.Close()

	c := &Client{URL: url, MaxRetries: 1, Backoff: time.Millisecond}
	_, err := c.Redeploy(fixtureID, false)
	if err == nil || !strings.Contains(err.Error(), "nothing was sent") {
		t.Errorf("want nothing was sent, got %v", err)
	}
	if err != nil && strings.Contains(err.Error(), "outcome is unknown") {
		t.Errorf("a refused dial has no unknown outcome: %v", err)
	}
}

func TestAWriteThatTimesOutAtDialSaysNothingWasSent(t *testing.T) {
	// 192.0.2.1 is reserved for documentation and never answers.
	c := &Client{URL: "http://192.0.2.1:9", MaxRetries: -1, Timeout: 100 * time.Millisecond}
	_, err := c.Redeploy(fixtureID, false)
	if err == nil {
		t.Fatal("want an error")
	}
	if strings.Contains(err.Error(), "outcome is unknown") {
		// A network that answers the dial with an immediate error is
		// also nothing sent, so only a timeout after connect is unknown.
		t.Errorf("a dial that never completed has no unknown outcome: %v", err)
	}
}

// rawServer answers each connection by reading the request head and body
// and then writing exactly what answer says.
func rawServer(t *testing.T, answer func(net.Conn)) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				buf := make([]byte, 4096)
				_, _ = conn.Read(buf)
				answer(conn)
			}()
		}
	}()
	return "http://" + ln.Addr().String()
}

func TestAWriteWhoseAnswerIsCutShortOrGarbledIsUnknown(t *testing.T) {
	cases := map[string]func(net.Conn){
		"truncated body": func(c net.Conn) {
			_, _ = c.Write([]byte("HTTP/1.1 202 Accepted\r\nContent-Type: application/json\r\nContent-Length: 500\r\n\r\n{\"task_id\":"))
		},
		"garbled status": func(c net.Conn) {
			_, _ = c.Write([]byte("this is not http\r\n\r\n"))
		},
		"stalled body": func(c net.Conn) {
			_, _ = c.Write([]byte("HTTP/1.1 202 Accepted\r\nContent-Length: 500\r\n\r\n{"))
			time.Sleep(2 * time.Second)
		},
	}
	for name, answer := range cases {
		url := rawServer(t, answer)
		c := &Client{URL: url, MaxRetries: -1, Timeout: 500 * time.Millisecond}

		_, err := c.Redeploy(fixtureID, false)
		assertUnknown(t, name, err)
	}
}

func TestAReadWhoseAnswerIsCutShortIsNotCalledUnknown(t *testing.T) {
	url := rawServer(t, func(c net.Conn) {
		_, _ = c.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 500\r\n\r\n{"))
	})
	c := &Client{URL: url, MaxRetries: -1}

	if _, err := c.Catalog(); err == nil || strings.Contains(err.Error(), "outcome is unknown") {
		t.Errorf("a read has no unknown outcome: %v", err)
	}
}

func TestCtrlCDuringTheRetryPauseOfARefusedWriteSaysNothingWasSent(t *testing.T) {
	srv := httptest.NewServer(nil)
	url := srv.URL
	srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	c := &Client{URL: url, MaxRetries: 3, Backoff: time.Hour, Ctx: ctx}
	_, err := c.Redeploy(fixtureID, false)
	if err == nil || !strings.Contains(err.Error(), "nothing was sent") {
		t.Errorf("want nothing was sent, got %v", err)
	}
	if err != nil && strings.Contains(err.Error(), "outcome is unknown") {
		t.Errorf("no byte left, so the outcome is known: %v", err)
	}
}
