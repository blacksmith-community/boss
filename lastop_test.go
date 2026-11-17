package main

import (
	"net/url"
	"strings"
	"testing"
)

// lastOperationQuery returns the query of the one last_operation poll a
// test's follow sent.
func lastOperationQuery(t *testing.T, b *fakeBroker, id string) url.Values {
	t.Helper()

	for _, q := range b.requests() {
		if q.Method == "GET" && strings.Contains(q.Path, "/v2/service_instances/"+id+"/last_operation") {
			u, err := url.Parse(q.Path)
			if err != nil {
				t.Fatal(err)
			}
			return u.Query()
		}
	}
	t.Fatalf("no last_operation poll in %+v", b.requests())
	return nil
}

func TestCreateFollowPollsWithTheOperationPlanAndService(t *testing.T) {
	b := osbBroker(t)
	b.route("PUT /v2/service_instances/new-one", func(int) reply { return reply{Status: 202, Body: `{"operation":"op-1"}`} })
	b.status("GET /v2/service_instances/new-one/last_operation", 200, "osb_last_operation_succeeded.json")
	r, _, _ := testRunner(b.client(), "", true)

	if err := r.create([]string{"redis/small"}, "new-one", "", true); err != nil {
		t.Fatal(err)
	}
	q := lastOperationQuery(t, b, "new-one")
	for key, want := range map[string]string{"operation": "op-1", "plan_id": "redis-small", "service_id": "redis"} {
		if q.Get(key) != want {
			t.Errorf("%s = %q, want %q (query %v)", key, q.Get(key), want, q)
		}
	}
}

func TestCreateFollowSendsNoOperationWhenTheBrokerGaveNone(t *testing.T) {
	b := osbBroker(t)
	b.status("GET /v2/service_instances/new-one/last_operation", 200, "osb_last_operation_succeeded.json")
	r, _, _ := testRunner(b.client(), "", true)

	if err := r.create([]string{"redis/small"}, "new-one", "", true); err != nil {
		t.Fatal(err)
	}
	q := lastOperationQuery(t, b, "new-one")
	if _, has := q["operation"]; has {
		t.Errorf("an empty operation was sent: %v", q)
	}
	if q.Get("plan_id") != "redis-small" || q.Get("service_id") != "redis" {
		t.Errorf("query %v", q)
	}
}

func TestUpdateFollowPollsWithTheNewPlanAndTheOperation(t *testing.T) {
	b := updatableBroker(t)
	b.route("PATCH /v2/service_instances/"+fixtureID, func(int) reply { return reply{Status: 202, Body: `{"operation":"op-2"}`} })
	b.status("GET /v2/service_instances/"+fixtureID+"/last_operation", 200, "osb_last_operation_succeeded.json")
	r, _, _ := testRunner(b.client(), "", true)

	if err := r.update([]string{fixtureID}, "", "large", true); err != nil {
		t.Fatal(err)
	}
	q := lastOperationQuery(t, b, fixtureID)
	for key, want := range map[string]string{"operation": "op-2", "plan_id": "redis-large", "service_id": "redis"} {
		if q.Get(key) != want {
			t.Errorf("%s = %q, want %q (query %v)", key, q.Get(key), want, q)
		}
	}
}

func TestUpdateOfParametersPollsWithTheCurrentPlan(t *testing.T) {
	b := osbBroker(t)
	b.status("GET /v2/service_instances/"+fixtureID+"/last_operation", 200, "osb_last_operation_succeeded.json")
	r, _, _ := testRunner(b.client(), "", true)

	if err := r.update([]string{fixtureID}, `{"k":"v"}`, "", true); err != nil {
		t.Fatal(err)
	}
	q := lastOperationQuery(t, b, fixtureID)
	if q.Get("plan_id") != "redis-plan" || q.Get("service_id") != "redis" {
		t.Errorf("query %v", q)
	}
}
