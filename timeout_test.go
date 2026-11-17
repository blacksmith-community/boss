package main

import (
	"strings"
	"testing"
	"time"
)

func TestTimeoutDefaults(t *testing.T) {
	c := Client{}
	if got := c.requestTimeout(); got != 30*time.Second {
		t.Errorf("a read defaults to %s, want 30s", got)
	}
	if got := c.long().requestTimeout(); got != 15*time.Minute {
		t.Errorf("a long call defaults to %s, want 15m", got)
	}

	c.Timeout = 2 * time.Minute
	if got := c.requestTimeout(); got != 2*time.Minute {
		t.Errorf("an explicit timeout gives %s", got)
	}
	if got := c.long().requestTimeout(); got != 2*time.Minute {
		t.Errorf("an explicit timeout also applies to a long call, got %s", got)
	}
}

func TestParseTimeout(t *testing.T) {
	for in, want := range map[string]time.Duration{"": 0, "90": 90 * time.Second, "5m": 5 * time.Minute, "1h30m": 90 * time.Minute} {
		got, err := parseTimeout(in)
		if err != nil || got != want {
			t.Errorf("parseTimeout(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"soon", "-5s", "0"} {
		if _, err := parseTimeout(bad); err == nil {
			t.Errorf("parseTimeout(%q) should fail", bad)
		}
	}
}

func TestAnErrandThatTimesOutSaysTheOutcomeIsUnknown(t *testing.T) {
	b := newBroker(t, nil)
	b.route("POST /b/deployments/dep-one/errands/smoke", func(int) reply {
		time.Sleep(400 * time.Millisecond)
		return reply{Status: 200, Body: `{}`}
	})

	c := retryClient(b)
	c.Timeout = 50 * time.Millisecond
	_, err := c.RunErrand("dep-one", "smoke", nil)
	if err == nil {
		t.Fatal("expected a timeout")
	}
	for _, want := range []string{"outcome is unknown", "boss tasks list"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "attempts") {
		t.Errorf("error must not talk about attempts: %v", err)
	}
	if n := count(b.requests(), "POST", "/b/deployments/dep-one/errands/smoke"); n != 1 {
		t.Errorf("errand was sent %d times, want 1", n)
	}
}

func TestA504OnRecreatePointsToTheTaskList(t *testing.T) {
	b := newBroker(t, nil)
	b.route("POST /b/"+fixtureID+"/recreate", func(int) reply {
		return reply{Status: 504, Body: `{"error":"timed out waiting","code":"outcome_unknown"}`}
	})

	_, err := b.client().Redeploy(fixtureID, false)
	if err == nil || !strings.Contains(err.Error(), "boss tasks list") {
		t.Errorf("want a pointer to boss tasks list, got %v", err)
	}
}

func TestAReadThatTimesOutIsNotCalledUnknown(t *testing.T) {
	b := newBroker(t, nil)
	b.route("GET /b/bosh/vm-monitor", func(int) reply {
		time.Sleep(300 * time.Millisecond)
		return reply{Status: 200, Body: `{}`}
	})

	c := b.client()
	c.Timeout = 50 * time.Millisecond
	_, err := c.VMMonitor()
	if err == nil || strings.Contains(err.Error(), "outcome is unknown") {
		t.Errorf("a read has no unknown outcome: %v", err)
	}
}
