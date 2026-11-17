package main

import (
	"errors"
	"strings"
	"testing"
)

const fixtureDeployment = "redis-small-11111111"

func deployBroker(t *testing.T) *fakeBroker {
	return newBroker(t, map[string]string{
		"GET /b/status": "status.json",
		"GET /b/deployments/" + fixtureDeployment + "/errands": "errands.json",
	})
}

func TestDeploymentOpsPostToTheirRoutes(t *testing.T) {
	for _, op := range []string{"restart", "recreate", "stop", "start"} {
		b := deployBroker(t)
		b.status("POST /b/deployments/"+fixtureDeployment+"/"+op, 200, "deployment_recreate.json")
		r, out, _ := testRunner(b.client(), "", true)

		if err := r.deploymentOp([]string{fixtureDeployment}, op); err != nil {
			t.Fatalf("%s: %v", op, err)
		}
		if posts := only(b.requests(), "POST"); len(posts) != 1 || posts[0].Body != "" {
			t.Errorf("%s posts: %+v", op, posts)
		}
		if !strings.Contains(out.String(), "BOSH task 5201") {
			t.Errorf("%s output: %s", op, out)
		}
	}
}

func TestDeploymentOpAcceptsAnInstanceID(t *testing.T) {
	b := deployBroker(t)
	b.status("POST /b/deployments/"+fixtureDeployment+"/stop", 200, "deployment_recreate.json")
	r, _, _ := testRunner(b.client(), "", true)

	if err := r.deploymentOp([]string{fixtureID}, "stop"); err != nil {
		t.Fatal(err)
	}
	if got := only(b.requests(), "POST")[0].Path; got != "/b/deployments/"+fixtureDeployment+"/stop" {
		t.Errorf("path: %s", got)
	}
}

func TestDeploymentOpsAskFirst(t *testing.T) {
	for _, op := range []string{"restart", "recreate", "stop", "start"} {
		b := deployBroker(t)
		b.status("POST /b/deployments/"+fixtureDeployment+"/"+op, 200, "deployment_recreate.json")
		r, _, errb := testRunner(b.client(), "n\n", false)

		if err := r.deploymentOp([]string{fixtureDeployment}, op); !errors.Is(err, errAborted) {
			t.Fatalf("%s: %v", op, err)
		}
		if len(only(b.requests(), "POST")) != 0 {
			t.Errorf("%s: a POST went out", op)
		}
		if !strings.Contains(errb.String(), fixtureDeployment) {
			t.Errorf("%s prompt: %s", op, errb)
		}
	}
}

func TestDeploymentOpReportsABusyDeployment(t *testing.T) {
	b := deployBroker(t)
	b.status("POST /b/deployments/"+fixtureDeployment+"/restart", 409, "deployment_busy.json")
	r, _, _ := testRunner(b.client(), "", true)

	err := r.deploymentOp([]string{fixtureDeployment}, "restart")
	if err == nil || !strings.Contains(err.Error(), "instance_busy") {
		t.Fatalf("got %v", err)
	}
	if len(only(b.requests(), "POST")) != 1 {
		t.Error("a refused POST was sent again")
	}
}

func TestErrandsListsNames(t *testing.T) {
	b := deployBroker(t)
	r, out, _ := testRunner(b.client(), "", true)

	if err := r.errands([]string{fixtureDeployment}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "smoke-tests\ncleanup\n") {
		t.Errorf("output: %s", out)
	}
}

func TestErrandRunsAndShowsOutput(t *testing.T) {
	b := deployBroker(t)
	b.status("POST /b/deployments/"+fixtureDeployment+"/errands/smoke-tests", 200, "errand_result.json")
	r, out, _ := testRunner(b.client(), "", true)

	if err := r.errand([]string{fixtureDeployment, "smoke-tests"}, []string{"standalone/aaaaaaaa", "other/1"}); err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"exited with 0", "all fixture checks passed"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if got := only(b.requests(), "POST")[0].Path; !strings.HasSuffix(got, "errands/smoke-tests?instances=standalone%2Faaaaaaaa%2Cother%2F1") {
		t.Errorf("path: %s", got)
	}
}

func TestErrandSkippedAndAskFirst(t *testing.T) {
	b := deployBroker(t)
	b.status("POST /b/deployments/"+fixtureDeployment+"/errands/smoke-tests", 200, "errand_skipped.json")

	r, _, _ := testRunner(b.client(), "n\n", false)
	if err := r.errand([]string{fixtureDeployment, "smoke-tests"}, nil); !errors.Is(err, errAborted) {
		t.Fatalf("got %v", err)
	}
	if len(only(b.requests(), "POST")) != 0 {
		t.Error("a POST went out after no")
	}

	r, out, _ := testRunner(b.client(), "", true)
	if err := r.errand([]string{fixtureDeployment, "smoke-tests"}, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "was skipped") {
		t.Errorf("output: %s", out)
	}
}

func TestErrandOutputIsNeverLogged(t *testing.T) {
	b := deployBroker(t)
	b.status("POST /b/deployments/"+fixtureDeployment+"/errands/smoke-tests", 200, "errand_result.json")
	c := b.client()
	var logged strings.Builder
	c.Debug, c.Stderr = true, &logged
	r, _, _ := testRunner(c, "", true)

	if err := r.errand([]string{fixtureDeployment, "smoke-tests"}, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logged.String(), "fixture stdout text") {
		t.Errorf("errand output leaked:\n%s", logged.String())
	}
}
