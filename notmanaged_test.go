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

func TestDeploymentInPath(t *testing.T) {
	for path, want := range map[string]string{
		"/b/deployments/fixture-dep/restart":     "fixture-dep",
		"/b/deployments/fixture-dep":             "fixture-dep",
		"/b/deployments/fixture-dep?x=1":         "fixture-dep",
		"/b/deployments/fixture-dep/errands?x=1": "fixture-dep",
		"/b/deployments/my%20dep%2Fone/restart":  "my dep/one",
		"/b/deployments/bad%zzname/restart":      "bad%zzname",
		"/b/" + fixtureID + "/restart":           "",
		"/b/deployments/":                        "",
		"/b/deployments":                         "",
		"/v2/service_instances/" + fixtureID:     "",
	} {
		if got := deploymentInPath(path); got != want {
			t.Errorf("%s: got %q, want %q", path, got, want)
		}
	}
}

func TestTheBrokersReasonFollowsBossOwnSentence(t *testing.T) {
	e := NotManagedError{Deployment: "fixture-dep", Message: "the broker will not change it"}
	got := e.Error()
	want := "the broker does not manage deployment fixture-dep, so it refused the request. The broker said: the broker will not change it"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if bare := (NotManagedError{Deployment: "fixture-dep"}).Error(); strings.Contains(bare, "said") {
		t.Errorf("an empty message adds nothing: %q", bare)
	}
}

func TestTheBrokersReasonShowsNoCredentialsOrControlCharacters(t *testing.T) {
	e := NotManagedError{Deployment: "d", Message: "see https://alice:hunter2@host.example/p?token=s3cret\nnow\x1b[31m"}
	got := e.Error()
	for _, leak := range []string{"alice", "hunter2", "s3cret", "token", "\n", "\x1b"} {
		if strings.Contains(got, leak) {
			t.Errorf("the message leaks %q: %q", leak, got)
		}
	}
	if !strings.Contains(got, "host.example/p") {
		t.Errorf("the message lost the URL host and path: %q", got)
	}
}

func TestTheBrokersRealReasonReachesTheOperator(t *testing.T) {
	b := newBroker(t, nil)
	b.status("POST /b/deployments/fixture-dep/restart", 403, "deployment_not_managed.json")
	r, _, _ := testRunner(b.client(), "", true)
	r.rawDeployment = "fixture-dep"

	err := r.deploymentOp(nil, "restart")
	if err == nil || !strings.Contains(err.Error(), "will not change it") {
		t.Fatalf("got %v, want the broker's reason", err)
	}
}
