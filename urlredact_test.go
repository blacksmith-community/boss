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
