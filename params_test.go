package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func osbBroker(t *testing.T) *fakeBroker {
	b := newBroker(t, map[string]string{
		"GET /v2/catalog": "catalog.json",
		"GET /b/status":   "status.json",
	})
	b.status("PUT /v2/service_instances/new-one", 202, "osb_delete.json")
	b.status("PATCH /v2/service_instances/"+fixtureID, 202, "osb_delete.json")
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

// updatableCatalog is the recorded catalog with plan_updateable switched
// on and the instance's own plan added. The broker never sets that field,
// so the recorded catalog alone cannot show the path that allows a change.
func updatableCatalog(t *testing.T) string {
	t.Helper()

	var cat map[string]interface{}
	if err := json.Unmarshal([]byte(fixture(t, "catalog.json")), &cat); err != nil {
		t.Fatal(err)
	}
	svc := cat["services"].([]interface{})[0].(map[string]interface{})
	svc["plan_updateable"] = true
	svc["plans"] = append(svc["plans"].([]interface{}), map[string]interface{}{"id": "redis-plan", "name": "standalone", "description": "fixture plan"})

	out, err := json.Marshal(cat)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func updatableBroker(t *testing.T) *fakeBroker {
	b := osbBroker(t)
	b.route("GET /v2/catalog", func(int) reply { return reply{Status: 200, Body: updatableCatalog(t)} })
	return b
}

func TestUpdateRefusesAPlanChangeTheCatalogDoesNotAllow(t *testing.T) {
	b := osbBroker(t)
	r, _, _ := testRunner(b.client(), "", true)

	err := r.update([]string{fixtureID}, "", "large", false)
	if err == nil || !strings.Contains(err.Error(), "plan_updateable") {
		t.Fatalf("got %v, want a refusal that names plan_updateable", err)
	}
	if len(only(b.requests(), "PATCH")) != 0 {
		t.Error("PATCH went out")
	}
}

func TestUpdateRefusesTheCurrentPlan(t *testing.T) {
	b := updatableBroker(t)
	r, _, _ := testRunner(b.client(), "", true)

	err := r.update([]string{fixtureID}, "", "standalone", false)
	if err == nil || !strings.Contains(err.Error(), "already uses") {
		t.Fatalf("got %v, want a refusal that the plan is unchanged", err)
	}
	if len(only(b.requests(), "PATCH")) != 0 {
		t.Error("PATCH went out")
	}
}

func TestUpdateSaysWhenTheBrokerCannotUpdate(t *testing.T) {
	b := osbBroker(t)
	b.status("PATCH /v2/service_instances/"+fixtureID, 500, "osb_update.json")
	r, _, _ := testRunner(b.client(), "", true)

	err := r.update([]string{fixtureID}, `{"a":1}`, "", false)
	if err == nil || !strings.Contains(err.Error(), "doesn't support updates yet") {
		t.Fatalf("got %v", err)
	}
	if len(only(b.requests(), "PATCH")) != 1 {
		t.Error("the update was sent more than once")
	}
}

func TestUpdateChangesThePlan(t *testing.T) {
	b := updatableBroker(t)
	r, _, _ := testRunner(b.client(), "", true)

	if err := r.update([]string{fixtureID}, "", "large", false); err != nil {
		t.Fatal(err)
	}

	body := lastBody(t, b, "PATCH")
	if body["plan_id"] != "redis-large" {
		t.Errorf("body: %v", body)
	}
	prev := body["previous_values"].(map[string]interface{})
	if prev["plan_id"] != "redis-plan" || prev["service_id"] != "redis" {
		t.Errorf("previous_values: %v", prev)
	}
}

func TestUpdateRejectsAnUnknownPlanAndAnEmptyUpdate(t *testing.T) {
	b := updatableBroker(t)
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

func TestParamsRefuseKeysThatAreNotStrings(t *testing.T) {
	for text, want := range map[string]string{
		"{\"a\": 1}\n":                 "",
		"1: one\n":                     "parameters key 1",
		"a:\n  true: yes\n":            "parameters.a key true",
		"a:\n  - b:\n      2: x\n":     "parameters.a[0].b key 2",
		"list:\n  - 1\n  - {3.5: z}\n": "parameters.list[1] key 3.5",
	} {
		_, err := parseParams(writeParams(t, text))
		switch {
		case want == "" && err != nil:
			t.Errorf("%q: %v", text, err)
		case want != "" && (err == nil || !strings.Contains(err.Error(), want)):
			t.Errorf("%q: got %v, want %q", text, err, want)
		}
	}
}

func writeParams(t *testing.T, text string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "p.yml")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
