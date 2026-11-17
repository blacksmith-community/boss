package main

import (
	"errors"
	"strings"
	"testing"
)

func TestDeleteAsksFirst(t *testing.T) {
	b := osbBroker(t)
	b.status("DELETE /v2/service_instances/"+fixtureID, 200, "osb_delete.json")
	r, _, errb := testRunner(b.client(), "n\n", false)

	if err := r.delete([]string{fixtureID}); !errors.Is(err, errAborted) {
		t.Fatalf("got %v", err)
	}
	if len(only(b.requests(), "DELETE")) != 0 {
		t.Error("DELETE went out")
	}
	if !strings.Contains(errb.String(), "Delete instance "+fixtureID) {
		t.Errorf("prompt: %s", errb)
	}
}

func TestDeleteWithYesDeletes(t *testing.T) {
	b := osbBroker(t)
	b.status("DELETE /v2/service_instances/"+fixtureID, 200, "osb_delete.json")
	r, out, _ := testRunner(b.client(), "", true)

	if err := r.delete([]string{"11111111-aaaa-bbbb-cccc-000000000001"}); err != nil {
		t.Fatal(err)
	}
	if got := only(b.requests(), "DELETE"); len(got) != 1 || !strings.Contains(got[0].Path, "accepts_incomplete=true") {
		t.Errorf("deletes: %+v", got)
	}
	if !strings.Contains(out.String(), "instance deleted") {
		t.Errorf("output: %s", out)
	}
}

func manyBroker(t *testing.T) *fakeBroker {
	b := newBroker(t, map[string]string{"GET /b/status": "status_many.json"})
	b.status("DELETE /v2/service_instances/"+fixtureID2, 202, "osb_delete.json")
	return b
}

func TestDeleteNeverGuessesFromAPrefix(t *testing.T) {
	b := manyBroker(t)
	r, _, _ := testRunner(b.client(), "", true)

	err := r.delete([]string{"happy-tu"})
	if err == nil || !strings.Contains(err.Error(), "no instance") {
		t.Fatalf("got %v, want a refusal", err)
	}
	if len(only(b.requests(), "DELETE")) != 0 {
		t.Error("a DELETE went out for a prefix")
	}
}

func TestDeleteResolvesAnExactNameAndSendsThePlan(t *testing.T) {
	b := manyBroker(t)
	r, out, _ := testRunner(b.client(), "", true)

	if err := r.delete([]string{"happy-turing"}); err != nil {
		t.Fatal(err)
	}
	got := only(b.requests(), "DELETE")
	if len(got) != 1 {
		t.Fatalf("deletes: %+v", got)
	}
	for _, want := range []string{"/v2/service_instances/" + fixtureID2, "accepts_incomplete=true", "plan_id=redis-plan", "service_id=redis"} {
		if !strings.Contains(got[0].Path, want) {
			t.Errorf("path %s lacks %s", got[0].Path, want)
		}
	}
	if !strings.Contains(out.String(), fixtureID2+" instance deleted") {
		t.Errorf("output: %s", out)
	}
}

func TestDeleteRefusesAnAmbiguousNameAnEmptyOneAndAMissingOne(t *testing.T) {
	for arg, want := range map[string]string{"shared-name": "matches 2 instances", "": "required", "not-indexed": "no instance"} {
		b := manyBroker(t)
		r, _, _ := testRunner(b.client(), "", true)

		err := r.delete([]string{arg})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got %v, want %q", arg, err, want)
		}
		if len(only(b.requests(), "DELETE")) != 0 {
			t.Errorf("%q: a DELETE went out", arg)
		}
	}
}

func TestDeleteRefusesWhenTheStatusCallFails(t *testing.T) {
	b := manyBroker(t)
	b.status("GET /b/status", 500, "action_unknown.json")
	r, _, _ := testRunner(b.client(), "", true)

	if err := r.delete([]string{fixtureID2}); err == nil {
		t.Fatal("delete went ahead without an index")
	}
	if len(only(b.requests(), "DELETE")) != 0 {
		t.Error("a DELETE went out")
	}
}

func TestDeleteOfAGoneInstanceFails(t *testing.T) {
	b := manyBroker(t)
	b.status("DELETE /v2/service_instances/"+fixtureID2, 410, "osb_delete_gone.json")
	r, out, _ := testRunner(b.client(), "", true)

	err := r.delete([]string{fixtureID2})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("got %v, want not found", err)
	}
	if strings.Contains(out.String(), "deleted") {
		t.Errorf("reported success: %s", out)
	}
}
