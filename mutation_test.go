package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync"
	"testing"
	"time"
)

// watching passes each request on unchanged and notes how long the
// request has to finish, which is the timeout the client chose for it.
type watching struct {
	mu        sync.Mutex
	remaining map[string]time.Duration
}

func (w *watching) RoundTrip(req *http.Request) (*http.Response, error) {
	if dl, ok := req.Context().Deadline(); ok {
		w.mu.Lock()
		w.remaining[req.Method+" "+req.URL.Path] = time.Until(dl)
		w.mu.Unlock()
	}
	return http.DefaultTransport.RoundTrip(req)
}

func (w *watching) limit(key string) time.Duration {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.remaining[key]
}

func TestLongCallsGetTheLongTimeoutAndReadsDoNot(t *testing.T) {
	b := newBroker(t, map[string]string{"GET /v2/catalog": "catalog.json", "GET /b/status": "status.json"})
	b.status("POST /b/deployments/dep-one/errands/smoke", 200, "errand_result.json")
	b.status("POST /b/deployments/dep-one/restart", 202, "deployment_restart.json")
	b.status("POST /b/"+fixtureID+"/recreate", 202, "action_recreate.json")
	b.status("DELETE /v2/service_instances/"+fixtureID, 202, "osb_delete.json")

	w := &watching{remaining: map[string]time.Duration{}}
	c := b.client()
	c.RoundTripper = w

	_, _ = c.RunErrand("dep-one", "smoke", nil)
	_, _ = c.DeploymentOp("dep-one", "restart")
	_, _ = c.Redeploy(fixtureID, false)
	_ = c.Delete(InstanceRef{ID: fixtureID, PlanID: "p", ServiceID: "s"})
	_, _ = c.Catalog()

	for _, key := range []string{
		"POST /b/deployments/dep-one/errands/smoke",
		"POST /b/deployments/dep-one/restart",
		"POST /b/" + fixtureID + "/recreate",
		"DELETE /v2/service_instances/" + fixtureID,
	} {
		if got := w.limit(key); got < 14*time.Minute {
			t.Errorf("%s may take %s, want about 15m", key, got)
		}
	}
	if got := w.limit("GET /v2/catalog"); got < 20*time.Second || got > 31*time.Second {
		t.Errorf("a read may take %s, want about 30s", got)
	}
}

func TestACancelledContextSendsNothing(t *testing.T) {
	b := newBroker(t, map[string]string{"GET /v2/catalog": "catalog.json"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := b.client()
	c.Ctx = ctx
	_, err := c.Catalog()
	if !errors.Is(err, context.Canceled) {
		t.Errorf("want the cancellation, got %v", err)
	}
	if n := len(b.requests()); n != 0 {
		t.Errorf("%d requests went out after the context ended", n)
	}
}

func TestAnErrandFailsWhenOneInstanceFailsAndTheTopLevelCodeIsZero(t *testing.T) {
	var res map[string]interface{}
	if err := json.Unmarshal([]byte(fixture(t, "errand_result.json")), &res); err != nil {
		t.Fatal(err)
	}
	res["results"] = []map[string]interface{}{
		{"instance": "node/0", "instance_id": "fixture-uuid-0", "exit_code": 0, "stdout": "ok", "stderr": ""},
		{"instance": "node/1", "instance_id": "fixture-uuid-1", "exit_code": 3, "stdout": "", "stderr": "boom"},
	}
	body, _ := json.Marshal(res)

	b := newBroker(t, nil)
	b.route("POST /b/deployments/dep-one/errands/smoke", func(int) reply { return reply{Status: 200, Body: string(body)} })
	r, _, _ := testRunner(b.client(), "", true)
	r.rawDeployment = "dep-one"

	err := r.errand([]string{"smoke"}, nil)
	if err == nil || !strings.Contains(err.Error(), "exited with 3") {
		t.Errorf("want the failing instance to fail the command, got %v", err)
	}
}

// scripted answers each attempt from a list of errors, and says a
// connection existed first when the script marks an error as sent.
type scripted struct {
	attempts int
	steps    []error
	sent     []bool
}

func (s *scripted) RoundTrip(req *http.Request) (*http.Response, error) {
	i := s.attempts
	s.attempts++
	if s.sent[i] {
		if tr := httptrace.ContextClientTrace(req.Context()); tr != nil && tr.GotConn != nil {
			tr.GotConn(httptrace.GotConnInfo{})
		}
	}
	return nil, s.steps[i]
}

func TestTheAttemptCountIsTheNumberOfRequestsMade(t *testing.T) {
	// The first attempt cannot connect, so it is tried again. The second
	// connects and then fails in a way that is not retried, so the error
	// must say 2 attempts even though the limit allows 6.
	s := &scripted{
		steps: []error{&net.OpError{Op: "dial", Err: errors.New("refused")}, errors.New("tls: bad certificate")},
		sent:  []bool{false, true},
	}
	var debug strings.Builder
	c := &Client{URL: "http://broker.invalid", MaxRetries: 5, Backoff: time.Millisecond, RoundTripper: s, Debug: true, Stderr: &debug}

	_, err := c.Redeploy(fixtureID, false)
	if err == nil || !strings.Contains(err.Error(), "after 2 attempts") {
		t.Fatalf("want 2 attempts, got %v", err)
	}
	if s.attempts != 2 {
		t.Errorf("%d requests were made, want 2", s.attempts)
	}
	if !strings.Contains(debug.String(), "attempt 2/6") {
		t.Errorf("the debug line should count the next attempt out of the limit:\n%s", debug.String())
	}
}
