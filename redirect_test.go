package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// elsewhere is a second server that records everything it is sent.
func elsewhere(t *testing.T) (*httptest.Server, *[]seen) {
	t.Helper()

	var mu sync.Mutex
	var got []seen
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = append(got, seen{Method: r.Method, Path: r.URL.RequestURI(), CommandKey: r.Header.Get(commandKeyHeader)})
		mu.Unlock()
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

// redirecting starts a broker whose route answers a redirect.
func redirecting(t *testing.T, key string, status int, target string) *fakeBroker {
	t.Helper()

	b := newBroker(t, nil)
	b.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method+" "+r.URL.Path == key {
			w.Header().Set("Location", target)
			w.WriteHeader(status)
			return
		}
		b.serve(w, r)
	})
	return b
}

func TestAPostRedirectIsNeverFollowed(t *testing.T) {
	for _, status := range []int{301, 302, 307, 308} {
		other, got := elsewhere(t)
		b := redirecting(t, "POST /b/upgrade/tasks/job-1/cancel", status, other.URL+"/b/upgrade/tasks/job-1/cancel")
		c := b.client()
		c.CommandKey = "the-key"

		_, err := c.BatchAction("job-1", "cancel")
		if err == nil || !strings.Contains(err.Error(), "redirect") {
			t.Errorf("status %d: want a refusal naming the redirect, got %v", status, err)
		}
		if len(*got) != 0 {
			t.Errorf("status %d: the other host was sent %v", status, *got)
		}
	}
}

func TestADeleteRedirectIsNotReportedAsADelete(t *testing.T) {
	other, got := elsewhere(t)
	b := newBroker(t, nil)
	b.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.Header().Set("Location", other.URL+r.URL.RequestURI())
			w.WriteHeader(301)
			return
		}
		b.serve(w, r)
	})

	_, _, err := b.client().rawRequest(http.MethodDelete, "/v2/service_instances/"+fixtureID, nil)
	if err == nil || !strings.Contains(err.Error(), "redirect") {
		t.Errorf("want the redirect refused, got %v", err)
	}
	if len(*got) != 0 {
		t.Errorf("the other host was sent %v", *got)
	}
}

func TestAReadNeverFollowsARedirectToAnotherHost(t *testing.T) {
	other, got := elsewhere(t)
	b := redirecting(t, "GET /v2/catalog", 302, other.URL+"/v2/catalog")

	_, err := b.client().Catalog()
	if err == nil || !strings.Contains(err.Error(), "redirect") {
		t.Errorf("want the redirect refused, got %v", err)
	}
	if len(*got) != 0 {
		t.Errorf("the other host was sent %v", *got)
	}
}

func TestAReadFollowsARedirectOnTheSameHost(t *testing.T) {
	b := newBroker(t, map[string]string{"GET /b/moved": "catalog.json"})
	b.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/catalog" {
			w.Header().Set("Location", "/b/moved")
			w.WriteHeader(302)
			return
		}
		b.serve(w, r)
	})

	cat, err := b.client().Catalog()
	if err != nil || len(cat.Services) == 0 {
		t.Errorf("want the catalog through the redirect, got %v %v", cat, err)
	}
}

func TestAReadRefusesARedirectThatChangesTheScheme(t *testing.T) {
	b := redirecting(t, "GET /v2/catalog", 302, "https://"+net.JoinHostPort("127.0.0.1", "1")+"/v2/catalog")

	_, err := b.client().Catalog()
	if err == nil || !strings.Contains(err.Error(), "redirect") {
		t.Errorf("want the redirect refused, got %v", err)
	}
}

func TestOneTransportServesEveryRequest(t *testing.T) {
	var conns int32
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fixture(t, "catalog.json")))
	}))
	srv.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateNew {
			atomic.AddInt32(&conns, 1)
		}
	}
	srv.Start()
	t.Cleanup(srv.Close)

	c := &Client{URL: srv.URL, MaxRetries: -1}
	for i := 0; i < 5; i++ {
		if _, err := c.Catalog(); err != nil {
			t.Fatal(err)
		}
	}
	if n := atomic.LoadInt32(&conns); n != 1 {
		t.Errorf("five reads opened %d connections, want 1", n)
	}
}

func TestAPostOnAReusedConnectionThatResetsIsNotSentAgain(t *testing.T) {
	var posts int32
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			atomic.AddInt32(&posts, 1)
			hj, _ := w.(http.Hijacker)
			conn, _, _ := hj.Hijack()
			_ = conn.Close()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	srv.Start()
	t.Cleanup(srv.Close)

	c := &Client{URL: srv.URL, MaxRetries: 2, Backoff: 1}
	if _, _, err := c.rawRequest(http.MethodGet, "/v2/catalog", nil); err != nil {
		t.Fatal(err)
	}
	_, _, err := c.rawRequest(http.MethodPost, "/b/x/recreate", map[string]string{})
	if err == nil {
		t.Fatal("want an error")
	}
	if n := atomic.LoadInt32(&posts); n != 1 {
		t.Errorf("the POST was sent %d times, want 1", n)
	}
}

// movedOnTheSameHost starts a broker whose every route for method answers
// status and a Location on the same host, and records what reaches the
// target.
func movedOnTheSameHost(t *testing.T, method string, status int) (*fakeBroker, *int32) {
	t.Helper()

	var reached int32
	b := newBroker(t, nil)
	b.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/moved":
			atomic.AddInt32(&reached, 1)
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == method:
			w.Header().Set("Location", "/moved")
			w.WriteHeader(status)
		default:
			b.serve(w, r)
		}
	})
	return b, &reached
}

func TestAnActionIsNeverTurnedIntoARead(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodDelete, http.MethodPut, http.MethodPatch} {
		for _, status := range []int{301, 302, 303, 307, 308} {
			b, reached := movedOnTheSameHost(t, method, status)

			_, _, err := b.client().rawRequest(method, "/v2/service_instances/"+fixtureID, map[string]string{"k": "v"})
			if err == nil || !strings.Contains(err.Error(), "redirect") {
				t.Errorf("%s %d: want the redirect refused, got %v", method, status, err)
			}
			if atomic.LoadInt32(reached) != 0 {
				t.Errorf("%s %d: the redirect target was reached", method, status)
			}
		}
	}
}

func TestARedirectKeepingTheHostButChangingTheSchemeIsRefused(t *testing.T) {
	b := newBroker(t, nil)
	b.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Same host and port, other scheme.
		host := strings.TrimPrefix(b.srv.URL, "http://")
		w.Header().Set("Location", "https://"+host+"/v2/catalog")
		w.WriteHeader(302)
	})

	if _, err := b.client().Catalog(); err == nil || !strings.Contains(err.Error(), "scheme or the host") {
		t.Errorf("want the scheme change refused, got %v", err)
	}
}

func TestARedirectChainStopsAtTenHops(t *testing.T) {
	var hits int32
	b := newBroker(t, nil)
	b.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Location", "/v2/catalog")
		w.WriteHeader(302)
	})

	_, err := b.client().Catalog()
	if err == nil || !strings.Contains(err.Error(), "too many redirects") {
		t.Fatalf("want too many redirects, got %v", err)
	}
	if n := atomic.LoadInt32(&hits); n != 10 {
		t.Errorf("the chain was followed %d times, want 10 requests", n)
	}
}
