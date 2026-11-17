package main

import (
	"strings"
	"testing"
)

func TestMonitorPrintsTheSummaryAndServices(t *testing.T) {
	b := newBroker(t, map[string]string{"GET /b/bosh/vm-monitor": "vm_monitor.json"})
	r, out, _ := testRunner(b.client(), "", true)

	if err := r.monitor(nil); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"checks 2 services every 1h0m0s", "1 healthy, 1 failing", "failing", "small-11111111-aaaa-bbbb-cccc-000000000001"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestMonitorSaysWhenItIsNotRunning(t *testing.T) {
	b := newBroker(t, map[string]string{"GET /b/bosh/vm-monitor": "vm_monitor_off.json"})
	r, out, _ := testRunner(b.client(), "", true)

	if err := r.monitor(nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "not running: VM monitor not configured") {
		t.Errorf("output: %s", out)
	}
}

func TestOrphansPrintsOrphansAndUnclaimed(t *testing.T) {
	b := newBroker(t, map[string]string{"GET /b/orphans": "orphans.json"})
	r, out, _ := testRunner(b.client(), "", true)

	if err := r.orphans(nil); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"33333333-aaaa-bbbb-cccc-000000000009", "2h0m0s", "due now", "removes an orphan after 24h0m0s", "small-44444444-aaaa-bbbb-cccc-000000000001"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestOrphansNeverCallsCleanup(t *testing.T) {
	b := newBroker(t, map[string]string{"GET /b/orphans": "orphans.json"})
	r, _, _ := testRunner(b.client(), "", true)

	if err := r.orphans(nil); err != nil {
		t.Fatal(err)
	}
	for _, req := range b.requests() {
		if req.Method != "GET" || strings.Contains(req.Path, "cleanup") {
			t.Errorf("unexpected request %+v", req)
		}
	}
}

func TestOrphansWhenNothingIsKnown(t *testing.T) {
	b := newBroker(t, map[string]string{"GET /b/orphans": "orphans_empty.json"})
	r, out, _ := testRunner(b.client(), "", true)

	if err := r.orphans(nil); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"No orphaned instances.", "not known yet"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}
