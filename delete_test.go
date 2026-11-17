package main

import (
	"errors"
	"strings"
	"testing"
)

func TestDeleteAsksFirst(t *testing.T) {
	b := osbBroker(t)
	b.status("DELETE /v2/service_instances/"+fixtureID, 200, "osb_empty.json")
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
	b.status("DELETE /v2/service_instances/"+fixtureID, 200, "osb_empty.json")
	r, out, _ := testRunner(b.client(), "", true)

	if err := r.delete([]string{"11111111-aaaa-bbbb-cccc-000000000001"}); err != nil {
		t.Fatal(err)
	}
	if got := only(b.requests(), "DELETE"); len(got) != 1 || !strings.HasSuffix(got[0].Path, "?accepts_incomplete=true") {
		t.Errorf("deletes: %+v", got)
	}
	if !strings.Contains(out.String(), "instance deleted") {
		t.Errorf("output: %s", out)
	}
}

func TestDeleteUsesAnIDTheIndexLacks(t *testing.T) {
	b := osbBroker(t)
	b.status("DELETE /v2/service_instances/not-indexed", 200, "osb_empty.json")
	r, _, _ := testRunner(b.client(), "", true)

	if err := r.delete([]string{"not-indexed"}); err != nil {
		t.Fatal(err)
	}
}
