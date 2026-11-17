package main

import (
	"errors"
	"strings"
	"testing"
)

func resBroker(t *testing.T) *fakeBroker {
	b := newBroker(t, map[string]string{"GET /b/status": "status.json"})
	b.status("PUT /b/"+fixtureID+"/resurrection", 200, "resurrection_on.json")
	b.status("DELETE /b/"+fixtureID+"/resurrection", 200, "resurrection_reset.json")
	b.status("PUT /b/blacksmith/resurrection", 200, "resurrection_off.json")
	return b
}

func TestResurrectionOnAndOffSendTheFlag(t *testing.T) {
	for action, want := range map[string]string{"on": `{"enabled":true}`, "off": `{"enabled":false}`} {
		b := resBroker(t)
		r, out, _ := testRunner(b.client(), "", true)

		if err := r.resurrection([]string{fixtureID}, action); err != nil {
			t.Fatalf("%s: %v", action, err)
		}
		if got := only(b.requests(), "PUT")[0].Body; got != want {
			t.Errorf("%s body: %s", action, got)
		}
		if !strings.Contains(out.String(), "Resurrection") {
			t.Errorf("%s output: %s", action, out)
		}
	}
}

func TestResurrectionResetDeletes(t *testing.T) {
	b := resBroker(t)
	r, out, _ := testRunner(b.client(), "", true)

	if err := r.resurrection([]string{fixtureID}, "reset"); err != nil {
		t.Fatal(err)
	}
	if len(only(b.requests(), "DELETE")) != 1 || !strings.Contains(out.String(), "config deleted") {
		t.Errorf("requests %+v output %s", b.requests(), out)
	}
}

func TestResurrectionAcceptsBlacksmithItself(t *testing.T) {
	b := resBroker(t)
	r, _, _ := testRunner(b.client(), "", true)

	if err := r.resurrection([]string{"blacksmith"}, "off"); err != nil {
		t.Fatal(err)
	}
	if len(only(b.requests(), "PUT")) != 1 {
		t.Error("expected one PUT")
	}
}

func TestResurrectionAsksFirst(t *testing.T) {
	for _, action := range []string{"on", "off", "reset"} {
		b := resBroker(t)
		r, _, _ := testRunner(b.client(), "n\n", false)

		if err := r.resurrection([]string{fixtureID}, action); !errors.Is(err, errAborted) {
			t.Fatalf("%s: %v", action, err)
		}
		if len(only(b.requests(), "PUT"))+len(only(b.requests(), "DELETE")) != 0 {
			t.Errorf("%s: a change went out", action)
		}
	}
}

func TestResurrectionReportsAFailureAndABadAction(t *testing.T) {
	b := resBroker(t)
	b.status("PUT /b/"+fixtureID+"/resurrection", 500, "resurrection_failed.json")
	r, _, _ := testRunner(b.client(), "", true)

	if err := r.resurrection([]string{fixtureID}, "on"); err == nil || !strings.Contains(err.Error(), "director refused") {
		t.Errorf("got %v", err)
	}

	var ue usageError
	if err := r.resurrection([]string{fixtureID}, "maybe"); !errors.As(err, &ue) {
		t.Errorf("got %v", err)
	}
}
