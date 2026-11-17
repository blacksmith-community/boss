package main

import (
	"strings"
	"testing"
)

func TestAnEmptyBatchJobIDIsRefusedBeforeAnythingIsSent(t *testing.T) {
	for _, arg := range []string{"", "  "} {
		for _, run := range map[string]func(*runner) error{
			"show":   func(r *runner) error { return r.batchShow([]string{arg}) },
			"cancel": func(r *runner) error { return r.batchAction([]string{arg}, "cancel") },
			"pause":  func(r *runner) error { return r.batchAction([]string{arg}, "pause") },
		} {
			b := newBroker(t, nil)
			r, _, _ := testRunner(b.client(), "", true)

			err := run(r)
			if err == nil || !strings.Contains(err.Error(), "job") {
				t.Errorf("job %q: want a refusal that names the job, got %v", arg, err)
			}
			if n := len(b.requests()); n != 0 {
				t.Errorf("job %q: %d requests went out", arg, n)
			}
		}
	}
}

func TestAnEmptyErrandNameIsRefusedBeforeAnythingIsSent(t *testing.T) {
	for _, name := range []string{"", " "} {
		b := newBroker(t, nil)
		r, _, _ := testRunner(b.client(), "", true)
		r.rawDeployment = "dep-one"

		err := r.errand([]string{name}, nil)
		if err == nil || !strings.Contains(err.Error(), "errand") {
			t.Errorf("errand %q: want a refusal that names the errand, got %v", name, err)
		}
		if n := len(b.requests()); n != 0 {
			t.Errorf("errand %q: %d requests went out", name, n)
		}
	}
}

func TestTheClientRefusesAnEmptyJobOrErrand(t *testing.T) {
	b := newBroker(t, nil)
	c := b.client()

	if _, err := c.BatchAction("", "cancel"); err == nil {
		t.Error("an empty job id must be refused")
	}
	if _, err := c.BatchTask(""); err == nil {
		t.Error("an empty job id must be refused")
	}
	if _, err := c.RunErrand("dep-one", "", nil); err == nil {
		t.Error("an empty errand must be refused")
	}
	if n := len(b.requests()); n != 0 {
		t.Errorf("%d requests went out", n)
	}
}
