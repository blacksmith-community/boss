package main

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// retryClient is a client that really retries, with a short backoff.
func retryClient(b *fakeBroker) *Client {
	c := b.client()
	c.MaxRetries = 2
	c.Backoff = time.Millisecond
	return c
}

func count(reqs []seen, method, path string) int {
	n := 0
	for _, r := range reqs {
		if r.Method == method && strings.HasPrefix(r.Path, path) {
			n++
		}
	}
	return n
}

func TestAnErrandThatAnswers500IsSentOnce(t *testing.T) {
	b := newBroker(t, nil)
	b.route("POST /b/deployments/dep-one/errands/smoke", func(int) reply {
		return reply{Status: 500, Body: `{"error":"errand failed"}`}
	})

	if _, err := retryClient(b).RunErrand("dep-one", "smoke", nil); err == nil {
		t.Fatal("expected an error")
	}
	if n := count(b.requests(), "POST", "/b/deployments/dep-one/errands/smoke"); n != 1 {
		t.Errorf("errand was sent %d times, want 1", n)
	}
}

func TestARecreateThatAnswers504IsSentOnce(t *testing.T) {
	b := newBroker(t, nil)
	b.route("POST /b/"+fixtureID+"/recreate", func(int) reply {
		return reply{Status: 504, Body: `{"error":"timed out","code":"outcome_unknown"}`}
	})

	if _, err := retryClient(b).Redeploy(fixtureID, false); err == nil {
		t.Fatal("expected an error")
	}
	if n := count(b.requests(), "POST", "/b/"+fixtureID+"/recreate"); n != 1 {
		t.Errorf("recreate was sent %d times, want 1", n)
	}
}

func TestAnUpdateThatAnswers500IsSentOnce(t *testing.T) {
	b := newBroker(t, nil)
	b.route("PATCH /v2/service_instances/"+fixtureID, func(int) reply {
		return reply{Status: 500, Body: `{"error":"not implemented"}`}
	})

	if _, err := retryClient(b).Update(fixtureID, "svc", "", "", map[string]interface{}{"a": 1}); err == nil {
		t.Fatal("expected an error")
	}
	if n := count(b.requests(), "PATCH", "/v2/service_instances/"+fixtureID); n != 1 {
		t.Errorf("update was sent %d times, want 1", n)
	}
}

func TestADeleteThatAnswers500IsSentOnce(t *testing.T) {
	b := newBroker(t, nil)
	b.route("DELETE /v2/service_instances/"+fixtureID, func(int) reply {
		return reply{Status: 500, Body: `{"error":"boom"}`}
	})

	if err := retryClient(b).Delete(InstanceRef{ID: fixtureID}); err == nil {
		t.Fatal("expected an error")
	}
	if n := count(b.requests(), "DELETE", "/v2/service_instances/"); n != 1 {
		t.Errorf("delete was sent %d times, want 1", n)
	}
}

func TestAReadThatAnswers500IsSentAgain(t *testing.T) {
	b := newBroker(t, map[string]string{"GET /b/bosh/vm-monitor": "vm_monitor.json"})
	b.route("GET /b/bosh/vm-monitor", func(n int) reply {
		if n == 1 {
			return reply{Status: 500, Body: `{"error":"try again"}`}
		}
		return reply{Status: 200, File: "vm_monitor.json"}
	})

	if _, err := retryClient(b).VMMonitor(); err != nil {
		t.Fatalf("the second answer should succeed: %v", err)
	}
	if n := count(b.requests(), "GET", "/b/bosh/vm-monitor"); n != 2 {
		t.Errorf("read was sent %d times, want 2", n)
	}
}

func TestAReadThatKeepsFailingStopsAfterTheLimit(t *testing.T) {
	b := newBroker(t, nil)
	b.status("GET /b/bosh/vm-monitor", 500, "")
	b.route("GET /b/bosh/vm-monitor", func(int) reply { return reply{Status: 500, Body: `{"error":"down"}`} })

	if _, err := retryClient(b).VMMonitor(); err == nil {
		t.Fatal("expected an error")
	}
	if n := count(b.requests(), "GET", "/b/bosh/vm-monitor"); n != 3 {
		t.Errorf("read was sent %d times, want 3", n)
	}
}

func TestARefusedConnectionIsRetriedForAnyMethod(t *testing.T) {
	srv := httptest.NewServer(nil)
	url := srv.URL
	srv.Close()

	c := &Client{URL: url, MaxRetries: 2, Backoff: time.Millisecond}
	_, err := c.Redeploy(fixtureID, false)
	if err == nil || !strings.Contains(err.Error(), "after 3 attempts") {
		t.Errorf("want a refused connection tried 3 times, got %v", err)
	}
}

func TestOneAttemptIsNotReportedAsMany(t *testing.T) {
	b := newBroker(t, nil)
	b.route("POST /b/"+fixtureID+"/recreate", func(int) reply { return reply{Status: 500, Body: `{"error":"x"}`} })

	_, err := retryClient(b).Redeploy(fixtureID, false)
	if err == nil || strings.Contains(err.Error(), "attempts") {
		t.Errorf("a single attempt must not say attempts: %v", err)
	}
}
