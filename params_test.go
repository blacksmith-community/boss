package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func osbBroker(t *testing.T) *fakeBroker {
	b := newBroker(t, map[string]string{
		"GET /v2/catalog": "catalog.json",
		"GET /b/status":   "status.json",
	})
	b.status("PUT /v2/service_instances/new-one", 202, "osb_empty.json")
	b.status("PATCH /v2/service_instances/"+fixtureID, 202, "osb_empty.json")
	return b
}

func lastBody(t *testing.T, b *fakeBroker, method string) map[string]interface{} {
	t.Helper()
	reqs := only(b.requests(), method)
	if len(reqs) != 1 {
		t.Fatalf("want one %s, got %d", method, len(reqs))
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(reqs[0].Body), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCreateSendsJSONParameters(t *testing.T) {
	b := osbBroker(t)
	r, out, _ := testRunner(b.client(), "", true)

	if err := r.create([]string{"redis/small"}, "new-one", `{"maxmemory":"256mb"}`, false); err != nil {
		t.Fatal(err)
	}

	body := lastBody(t, b, "PUT")
	if body["service_id"] != "redis" || body["plan_id"] != "redis-small" {
		t.Errorf("body: %v", body)
	}
	if body["parameters"].(map[string]interface{})["maxmemory"] != "256mb" {
		t.Errorf("parameters: %v", body["parameters"])
	}
	if body["organization_guid"] != "boss" || body["space_guid"] != "boss" {
		t.Errorf("org and space: %v", body)
	}
	if !strings.Contains(out.String(), "instance new-one created") {
		t.Errorf("output: %s", out)
	}
	if got := only(b.requests(), "PUT")[0].Path; !strings.HasSuffix(got, "?accepts_incomplete=true") {
		t.Errorf("path: %s", got)
	}
}

func TestCreateReadsParametersFromJSONAndYAMLFiles(t *testing.T) {
	for _, file := range []string{"testdata/params.json", "testdata/params.yml"} {
		b := osbBroker(t)
		r, _, _ := testRunner(b.client(), "", true)

		if err := r.create([]string{"redis/small"}, "new-one", file, false); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		p := lastBody(t, b, "PUT")["parameters"].(map[string]interface{})
		if p["maxmemory"] != "256mb" || p["nested"].(map[string]interface{})["flag"] != true {
			t.Errorf("%s: parameters %v", file, p)
		}
	}
}

func TestCreateRejectsBadParametersBeforeAnyRequest(t *testing.T) {
	b := osbBroker(t)
	r, _, _ := testRunner(b.client(), "", true)

	for _, bad := range []string{`{"a":`, "testdata/no-such-file.json"} {
		if err := r.create([]string{"redis/small"}, "x", bad, false); err == nil {
			t.Errorf("%q: expected an error", bad)
		}
	}
	if len(b.requests()) != 0 {
		t.Errorf("requests went out: %+v", b.requests())
	}
}

func TestCreateAsksFirst(t *testing.T) {
	b := osbBroker(t)
	r, _, errb := testRunner(b.client(), "\n", false)

	err := r.create([]string{"redis/small"}, "new-one", "", false)
	if !errors.Is(err, errAborted) {
		t.Fatalf("got %v", err)
	}
	if len(only(b.requests(), "PUT")) != 0 {
		t.Error("PUT went out")
	}
	if !strings.Contains(errb.String(), "org boss and space boss") {
		t.Errorf("prompt: %s", errb)
	}
}

func TestUpdateSendsParameters(t *testing.T) {
	b := osbBroker(t)
	r, _, _ := testRunner(b.client(), "", true)

	if err := r.update([]string{fixtureID}, `{"maxmemory":"512mb"}`, "", false); err != nil {
		t.Fatal(err)
	}

	body := lastBody(t, b, "PATCH")
	if body["service_id"] != "redis" || body["parameters"].(map[string]interface{})["maxmemory"] != "512mb" {
		t.Errorf("body: %v", body)
	}
	if _, has := body["plan_id"]; has {
		t.Errorf("plan_id sent without a plan change: %v", body)
	}
}

func TestUpdateChangesThePlan(t *testing.T) {
	b := osbBroker(t)
	r, _, _ := testRunner(b.client(), "", true)

	if err := r.update([]string{fixtureID}, "", "large", false); err != nil {
		t.Fatal(err)
	}

	body := lastBody(t, b, "PATCH")
	if body["plan_id"] != "redis-large" {
		t.Errorf("body: %v", body)
	}
	prev := body["previous_values"].(map[string]interface{})
	if prev["plan_id"] != "redis-small" || prev["service_id"] != "redis" {
		t.Errorf("previous_values: %v", prev)
	}
}

func TestUpdateRejectsAnUnknownPlanAndAnEmptyUpdate(t *testing.T) {
	b := osbBroker(t)
	r, _, _ := testRunner(b.client(), "", true)

	if err := r.update([]string{fixtureID}, "", "huge", false); err == nil || !strings.Contains(err.Error(), "no plan 'huge'") {
		t.Errorf("got %v", err)
	}
	var ue usageError
	if err := r.update([]string{fixtureID}, "", "", false); !errors.As(err, &ue) {
		t.Errorf("got %v, want a usage error", err)
	}
	if len(only(b.requests(), "PATCH")) != 0 {
		t.Error("PATCH went out")
	}
}

func TestUpdateAsksFirst(t *testing.T) {
	b := osbBroker(t)
	r, _, _ := testRunner(b.client(), "no\n", false)

	if err := r.update([]string{fixtureID}, `{"a":1}`, "", false); !errors.Is(err, errAborted) {
		t.Fatalf("got %v", err)
	}
	if len(only(b.requests(), "PATCH")) != 0 {
		t.Error("PATCH went out")
	}
}

func TestDebugHidesProvisionParameters(t *testing.T) {
	b := osbBroker(t)
	c := b.client()
	var logged strings.Builder
	c.Debug, c.Stderr = true, &logged
	r, _, _ := testRunner(c, "", true)

	if err := r.create([]string{"redis/small"}, "new-one", `{"password":"fixture-param-value"}`, false); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logged.String(), "fixture-param-value") {
		t.Errorf("parameters leaked:\n%s", logged.String())
	}
}
