package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestARedirectErrorHidesTheUserinfoAndQueryOfTheTarget(t *testing.T) {
	b := redirecting(t, "GET /v2/catalog", 302, "http://alice:hunter2@other.example/v2/catalog?token=s3cret&x=1")

	_, err := b.client().Catalog()
	if err == nil || !strings.Contains(err.Error(), "other.example/v2/catalog") {
		t.Fatalf("want the refusal to name the target host and path, got %v", err)
	}
	for _, leak := range []string{"alice", "hunter2", "s3cret", "token", "x=1"} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("the error leaks %q: %v", leak, err)
		}
	}
}

func TestAFailedRequestHidesTheUserinfoAndQueryOfItsURL(t *testing.T) {
	b := newBroker(t, nil)
	b.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _, _ := w.(http.Hijacker).Hijack()
		_ = conn.Close()
	})
	c := b.client()
	c.URL = strings.Replace(c.URL, "http://", "http://alice:hunter2@", 1)

	_, err := c.LastOperation(fixtureID, OperationRef{Operation: "op-secret-token", PlanID: "p", ServiceID: "s"})
	if err == nil {
		t.Fatal("want an error")
	}
	for _, leak := range []string{"alice", "hunter2", "op-secret-token", "operation="} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("the error leaks %q: %v", leak, err)
		}
	}
}

func TestABrokerURLThatWillNotParseNeverShowsItsPassword(t *testing.T) {
	for _, bad := range []string{
		"http://bob:SECRETPW@host:badport",
		"http://bob:SECRETPW@host:80/a%zz",
		"http://bob:SECRETPW@[::1",
	} {
		c := &Client{URL: bad, MaxRetries: -1}
		for name, call := range map[string]func() error{
			"read":  func() error { _, err := c.Catalog(); return err },
			"write": func() error { _, err := c.Redeploy(fixtureID, false); return err },
		} {
			err := call()
			if err == nil {
				t.Fatalf("%s %s: want an error", bad, name)
			}
			for _, leak := range []string{"SECRETPW", "bob"} {
				if strings.Contains(err.Error(), leak) {
					t.Errorf("%s %s: the error leaks %q: %v", bad, name, leak, err)
				}
			}
		}
	}
}
