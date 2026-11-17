package main

import (
	"errors"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestARefusedRedirectIsNeverRetried(t *testing.T) {
	var hits int32
	b := newBroker(t, nil)
	b.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Location", "http://other.example/timeout/temporary failure")
		w.WriteHeader(302)
	})
	c := b.client()
	c.MaxRetries = 3
	c.Backoff = time.Millisecond

	if _, err := c.Catalog(); err == nil || !strings.Contains(err.Error(), "redirect") {
		t.Fatalf("want the redirect refused, got %v", err)
	}
	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Errorf("the broker was asked %d times, want 1", n)
	}
}

func TestRetryDependsOnTheTypeOfTheErrorNotItsText(t *testing.T) {
	for name, tc := range map[string]struct {
		err   error
		retry bool
	}{
		"text that says timeout":            {errors.New("timeout waiting for something"), false},
		"text that says temporary failure":  {errors.New("temporary failure in name resolution"), false},
		"text that says connection refused": {errors.New("connection refused by the proxy"), false},
		"a timeout error":                   {&net.OpError{Op: "read", Err: timeoutError{}}, true},
		"a temporary DNS failure":           {&net.DNSError{Err: "no such host", IsTemporary: true}, true},
		"a DNS answer that is final":        {&net.DNSError{Err: "no such host", IsNotFound: true}, false},
	} {
		s := &scripted{steps: []error{tc.err, errors.New("second")}, sent: []bool{true, true}}
		c := &Client{URL: "http://broker.invalid", MaxRetries: 1, Backoff: time.Millisecond, RoundTripper: s}

		_, _ = c.Catalog()
		if got := s.attempts == 2; got != tc.retry {
			t.Errorf("%s: %d attempts, retry should be %v", name, s.attempts, tc.retry)
		}
	}
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o deadline" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

func TestAGivenUpReadSaysSoOnceWithoutRepeatedPrefixes(t *testing.T) {
	s := &scripted{steps: []error{timeoutError{}, timeoutError{}}, sent: []bool{true, true}}
	c := &Client{URL: "http://broker.invalid", MaxRetries: 1, Backoff: time.Millisecond, RoundTripper: s}

	_, err := c.Catalog()
	if err == nil {
		t.Fatal("want an error")
	}
	if strings.Count(err.Error(), "request failed") > 1 || strings.Contains(err.Error(), "HTTP request failed") {
		t.Errorf("the message repeats itself: %v", err)
	}
	if !strings.Contains(err.Error(), "after 2 attempts") {
		t.Errorf("the message lacks the attempt count: %v", err)
	}
}
