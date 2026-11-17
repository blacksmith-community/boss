package main

import (
	"errors"
	"strings"
	"testing"
)

func TestARawDeploymentTheBrokerDoesNotManageIsNamedAndFails(t *testing.T) {
	b := newBroker(t, nil)
	b.status("POST /b/deployments/fixture-dep/restart", 403, "deployment_not_managed.json")
	r, _, _ := testRunner(b.client(), "", true)
	r.rawDeployment = "fixture-dep"

	err := r.deploymentOp(nil, "restart")
	var nm NotManagedError
	if !errors.As(err, &nm) {
		t.Fatalf("got %v, want a NotManagedError", err)
	}
	if nm.Deployment != "fixture-dep" || !strings.Contains(err.Error(), "does not manage") || !strings.Contains(err.Error(), "fixture-dep") {
		t.Errorf("message: %v", err)
	}
}

func TestRedeployFallbackToADeploymentTheBrokerDoesNotManageFails(t *testing.T) {
	b := newBroker(t, map[string]string{"GET /b/status": "v182_status.json"})
	b.route("POST /b/"+fixtureID+"/recreate", func(int) reply {
		return reply{Status: 404, File: "v182_instance_action_not_found.txt", ContentType: "text/plain"}
	})
	b.status("POST /b/deployments/redis-small-11111111/recreate", 403, "deployment_not_managed.json")
	r, _, _ := testRunner(b.client(), "", true)

	var nm NotManagedError
	if err := r.redeploy([]string{fixtureID}, false, false); !errors.As(err, &nm) {
		t.Fatalf("got %v, want a NotManagedError", err)
	}
}

func TestOtherForbiddenAnswersKeepTheirOwnMessage(t *testing.T) {
	b := newBroker(t, nil)
	b.route("POST /b/deployments/fixture-dep/restart", func(int) reply {
		return reply{Status: 403, Body: `{"code":"forbidden","error":"nope","success":false}`}
	})
	r, _, _ := testRunner(b.client(), "", true)
	r.rawDeployment = "fixture-dep"

	err := r.deploymentOp(nil, "restart")
	var nm NotManagedError
	if err == nil || errors.As(err, &nm) {
		t.Fatalf("got %v, want the plain API error", err)
	}
}

func TestOnlyTheCodeMakesAnAnswerNotManaged(t *testing.T) {
	for name, tc := range map[string]struct {
		body string
		want bool
	}{
		"the code with a message that never says it": {`{"code":"deployment_not_managed","error":"refused for reasons of its own","success":false}`, true},
		"another code whose text says not managed":   {`{"code":"forbidden","error":"deployment fixture-dep is not managed by this broker","success":false}`, false},
	} {
		b := newBroker(t, nil)
		b.route("POST /b/deployments/fixture-dep/restart", func(int) reply { return reply{Status: 403, Body: tc.body} })
		r, _, _ := testRunner(b.client(), "", true)
		r.rawDeployment = "fixture-dep"

		var nm NotManagedError
		err := r.deploymentOp(nil, "restart")
		if got := errors.As(err, &nm); got != tc.want || err == nil {
			t.Errorf("%s: got %v, NotManagedError is %v, want %v", name, err, got, tc.want)
		}
	}
}
