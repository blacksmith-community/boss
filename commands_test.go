package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

const (
	fixtureID  = "11111111-aaaa-bbbb-cccc-000000000001"
	fixtureID2 = "11111111-aaaa-bbbb-cccc-000000000002"
)

func TestTaskReadsTheTaskLogRoute(t *testing.T) {
	b := newBroker(t, map[string]string{
		"GET /b/status":                     "status.json",
		"GET /b/" + fixtureID + "/task/log": "task_log.json",
	})
	r, out, _ := testRunner(b.client(), "", true)

	if err := r.task([]string{"11111111-aaaa-bbbb-cccc-000000000001"}, false); err != nil {
		t.Fatal(err)
	}

	got := out.String()
	for _, want := range []string{"# " + fixtureID, "08:00:00 | Updating instance: redis/0 started", "08:01:00 | Updating instance: redis/0 finished"} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %q:\n%s", want, got)
		}
	}
}

func TestTaskResolvesAnExactNameAndRefusesAPrefix(t *testing.T) {
	b := newBroker(t, map[string]string{"GET /b/status": "status_many.json"})
	b.status("GET /b/"+fixtureID2+"/task/log", 200, "task_log.json")
	r, _, _ := testRunner(b.client(), "", true)

	if err := r.task([]string{"happy-turing"}, false); err != nil {
		t.Fatal(err)
	}
	if err := r.task([]string{"happy-tu"}, false); err == nil {
		t.Fatal("a name prefix was accepted")
	}
	if err := r.task([]string{"22222222"}, false); err == nil {
		t.Fatal("an ID prefix was accepted")
	}
}

func TestResolveRefusesAnAmbiguousName(t *testing.T) {
	b := newBroker(t, map[string]string{"GET /b/status": "status_many.json"})

	_, err := b.client().Resolve("shared-name")
	if err == nil || !strings.Contains(err.Error(), "matches 2 instances") {
		t.Fatalf("want an ambiguity error, got %v", err)
	}
}

func TestResolveRefusesAnEmptyArgument(t *testing.T) {
	b := newBroker(t, map[string]string{"GET /b/status": "status_many.json"})

	if _, err := b.client().Resolve(""); err == nil {
		t.Fatal("an empty argument resolved")
	}
}

func TestTaskWithoutEventsSaysSo(t *testing.T) {
	b := newBroker(t, map[string]string{"GET /b/status": "status.json"})
	b.route("GET /b/"+fixtureID+"/task/log", func(int) reply { return reply{Status: 200, Body: "[]"} })
	r, out, _ := testRunner(b.client(), "", true)

	if err := r.task([]string{fixtureID}, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "No task events yet.") {
		t.Errorf("output: %s", out)
	}
}

func TestManifestReadsTheManifestRoute(t *testing.T) {
	b := newBroker(t, map[string]string{
		"GET /b/status":                     "status.json",
		"GET /b/" + fixtureID + "/manifest": "manifest.json",
	})
	r, out, _ := testRunner(b.client(), "", true)

	if err := r.manifest([]string{fixtureID}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "instance_groups:") || !strings.Contains(out.String(), "# "+fixtureID) {
		t.Errorf("output: %s", out)
	}
}

func TestManifestReportsTheBrokersReason(t *testing.T) {
	b := newBroker(t, map[string]string{
		"GET /b/status":                     "status.json",
		"GET /b/" + fixtureID + "/manifest": "manifest_error.json",
	})
	r, _, _ := testRunner(b.client(), "", true)

	err := r.manifest([]string{fixtureID})
	if err == nil || !strings.Contains(err.Error(), "service instance not found") {
		t.Fatalf("got %v", err)
	}
}

func TestCredsReadsTheCredentialsRoute(t *testing.T) {
	b := newBroker(t, map[string]string{
		"GET /b/status":                        "status.json",
		"GET /b/" + fixtureID + "/credentials": "credentials.json",
	})
	r, out, _ := testRunner(b.client(), "", true)

	if err := r.creds([]string{fixtureID}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"host: 10.0.0.5", "port: 6379", "password: fixture-not-a-secret"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestCredsSurfacesABrokerErrorBody(t *testing.T) {
	b := newBroker(t, map[string]string{
		"GET /b/status":                        "status.json",
		"GET /b/" + fixtureID + "/credentials": "credentials_error.json",
	})
	r, out, _ := testRunner(b.client(), "", true)

	err := r.creds([]string{fixtureID})
	if err == nil || !strings.Contains(err.Error(), "vault unavailable") {
		t.Fatalf("got %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("printed output on error: %s", out)
	}
}

func TestCredsRefusesTheBrokersPlaceholder(t *testing.T) {
	b := newBroker(t, map[string]string{
		"GET /b/status":                        "status.json",
		"GET /b/" + fixtureID + "/credentials": "credentials_placeholder.json",
	})
	r, out, _ := testRunner(b.client(), "", true)

	err := r.creds([]string{fixtureID})
	if err == nil || !strings.Contains(err.Error(), "placeholder") {
		t.Fatalf("got %v, want a refusal that names the placeholder", err)
	}
	if out.Len() != 0 {
		t.Errorf("printed the placeholder as if it were credentials: %s", out)
	}
}

func TestLogReadsTheBrokerLogRoute(t *testing.T) {
	b := newBroker(t, map[string]string{"GET /b/blacksmith/logs": "logs.json"})
	r, out, _ := testRunner(b.client(), "", true)

	if err := r.log(nil, LogOptions{File: "/var/vcap/sys/log/blacksmith/blacksmith.stderr.log", Limit: 50, Offset: 5}); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out.String(), "fixture log line 5") {
		t.Errorf("output: %s", out)
	}
	got := b.requests()[0].Path
	for _, want := range []string{"file=%2Fvar%2Fvcap%2Fsys%2Flog%2Fblacksmith%2Fblacksmith.stderr.log", "limit=50", "offset=5"} {
		if !strings.Contains(got, want) {
			t.Errorf("request %s lacks %s", got, want)
		}
	}
}

func TestLogReportsAnErrorTheBrokerSendsWith200(t *testing.T) {
	b := newBroker(t, map[string]string{"GET /b/blacksmith/logs": "logs_denied.json"})
	r, _, _ := testRunner(b.client(), "", true)

	err := r.log(nil, LogOptions{})
	if err == nil || !strings.Contains(err.Error(), "unauthorized log file access") {
		t.Fatalf("got %v", err)
	}
}

func TestRedeployRecreatesByDefault(t *testing.T) {
	b := newBroker(t, map[string]string{"GET /b/status": "status.json"})
	b.status("POST /b/"+fixtureID+"/recreate", 202, "action_recreate.json")
	r, out, _ := testRunner(b.client(), "", true)

	if err := r.redeploy([]string{fixtureID}, false, false); err != nil {
		t.Fatal(err)
	}

	posts := only(b.requests(), "POST")
	if len(posts) != 1 || posts[0].Path != "/b/"+fixtureID+"/recreate" {
		t.Fatalf("posts: %+v", posts)
	}
	if !strings.Contains(out.String(), "BOSH task 4321") {
		t.Errorf("output: %s", out)
	}
}

func TestRedeployRestartUsesTheRestartRoute(t *testing.T) {
	b := newBroker(t, map[string]string{"GET /b/status": "status.json"})
	b.status("POST /b/"+fixtureID+"/restart", 202, "action_restart.json")
	r, out, _ := testRunner(b.client(), "", true)

	if err := r.redeploy([]string{fixtureID}, true, false); err != nil {
		t.Fatal(err)
	}

	posts := only(b.requests(), "POST")
	if len(posts) != 1 || posts[0].Path != "/b/"+fixtureID+"/restart" {
		t.Fatalf("posts: %+v", posts)
	}
	if !strings.Contains(out.String(), "BOSH task 4321") {
		t.Errorf("output: %s", out)
	}
}

func TestRedeployFallsBackToTheDeploymentRoutesOnAnOldBroker(t *testing.T) {
	for _, action := range []string{"recreate", "restart"} {
		b := newBroker(t, map[string]string{"GET /b/status": "v182_status.json"})
		b.route("POST /b/"+fixtureID+"/"+action, func(int) reply {
			return reply{Status: 404, File: "v182_instance_action_not_found.txt", ContentType: "text/plain; charset=utf-8"}
		})
		b.status("POST /b/deployments/redis-small-11111111/"+action, 200, "v182_deployment_"+action+".json")
		r, out, _ := testRunner(b.client(), "", true)

		if err := r.redeploy([]string{fixtureID}, action == "restart", false); err != nil {
			t.Fatalf("%s: %v", action, err)
		}
		posts := only(b.requests(), "POST")
		if len(posts) != 2 || posts[1].Path != "/b/deployments/redis-small-11111111/"+action {
			t.Fatalf("%s posts: %+v", action, posts)
		}
		if !strings.Contains(out.String(), "BOSH task 5201") {
			t.Errorf("%s output: %s", action, out)
		}
	}
}

func TestRedeployOnAnOldBrokerWithNoDeploymentRoutesSaysSo(t *testing.T) {
	b := newBroker(t, map[string]string{"GET /b/status": "v182_status.json"})
	b.route("POST /b/"+fixtureID+"/recreate", func(int) reply {
		return reply{Status: 404, File: "v182_instance_action_not_found.txt", ContentType: "text/plain"}
	})
	b.route("POST /b/deployments/redis-small-11111111/recreate", func(int) reply {
		return reply{Status: 404, File: "v182_endpoint_not_found.txt", ContentType: "text/plain"}
	})
	r, _, _ := testRunner(b.client(), "", true)

	var old BrokerTooOldError
	if err := r.redeploy([]string{fixtureID}, false, false); !errors.As(err, &old) {
		t.Fatalf("got %v, want a BrokerTooOldError", err)
	}
}

func TestRedeployAsksFirstAndHonorsNo(t *testing.T) {
	b := newBroker(t, map[string]string{"GET /b/status": "status.json"})
	b.status("POST /b/"+fixtureID+"/recreate", 202, "action_recreate.json")
	r, _, errb := testRunner(b.client(), "n\n", false)

	err := r.redeploy([]string{fixtureID}, false, false)
	if !errors.Is(err, errAborted) {
		t.Fatalf("got %v, want errAborted", err)
	}
	if len(only(b.requests(), "POST")) != 0 {
		t.Error("a POST went out after the operator said no")
	}
	if !strings.Contains(errb.String(), "Recreate the VMs of instance "+fixtureID) {
		t.Errorf("prompt: %s", errb)
	}
}

func TestRedeployProceedsOnYes(t *testing.T) {
	b := newBroker(t, map[string]string{"GET /b/status": "status.json"})
	b.status("POST /b/"+fixtureID+"/recreate", 202, "action_recreate.json")
	r, _, _ := testRunner(b.client(), "yes\n", false)

	if err := r.redeploy([]string{fixtureID}, false, false); err != nil {
		t.Fatal(err)
	}
	if len(only(b.requests(), "POST")) != 1 {
		t.Error("expected one POST")
	}
}

func TestRedeployReportsALockedDeployment(t *testing.T) {
	b := newBroker(t, map[string]string{"GET /b/status": "status.json"})
	b.status("POST /b/"+fixtureID+"/recreate", 409, "action_locked.json")
	r, _, _ := testRunner(b.client(), "", true)

	err := r.redeploy([]string{fixtureID}, false, false)
	if err == nil || !strings.Contains(err.Error(), "running task 812") || !strings.Contains(err.Error(), "deployment_locked") {
		t.Fatalf("got %v", err)
	}
	if len(only(b.requests(), "POST")) != 1 {
		t.Error("a refused POST must not be sent again")
	}
}

func TestCommandsDemandAnInstance(t *testing.T) {
	r, _, _ := testRunner(&Client{}, "", true)

	for name, err := range map[string]error{
		"task":     r.task(nil, false),
		"manifest": r.manifest(nil),
		"creds":    r.creds(nil),
		"redeploy": r.redeploy(nil, false, false),
		"log":      r.log([]string{"x"}, LogOptions{}),
	} {
		var ue usageError
		if !errors.As(err, &ue) {
			t.Errorf("%s: got %v, want a usage error", name, err)
		}
	}
}

func TestDebugAndTraceNeverShowCredentials(t *testing.T) {
	b := newBroker(t, map[string]string{
		"GET /b/status":                        "status.json",
		"GET /b/" + fixtureID + "/credentials": "credentials.json",
		"GET /b/" + fixtureID + "/manifest":    "manifest.json",
	})
	c := b.client()
	var logged bytes.Buffer
	c.Debug, c.Trace, c.Stderr = true, true, &logged
	c.CommandKey = "fixture-command-key"
	r, _, _ := testRunner(c, "", true)

	if err := r.creds([]string{fixtureID}); err != nil {
		t.Fatal(err)
	}
	if err := r.manifest([]string{fixtureID}); err != nil {
		t.Fatal(err)
	}

	got := logged.String()
	for _, secret := range []string{"fixture-not-a-secret", "fixture-pass", "fixture-command-key", "Zml4dHVyZS11c2VyOmZpeHR1cmUtcGFzcw"} {
		if strings.Contains(got, secret) {
			t.Errorf("the log shows %q:\n%s", secret, got)
		}
	}
	if !strings.Contains(got, "[redacted]") {
		t.Errorf("expected redaction markers in:\n%s", got)
	}
}

func TestQueryStringSurvivesOnOSBCalls(t *testing.T) {
	b := newBroker(t, map[string]string{})
	b.status("DELETE /v2/service_instances/"+fixtureID, 200, "")
	b.route("DELETE /v2/service_instances/"+fixtureID, func(int) reply { return reply{Status: 200, Body: "{}"} })

	if err := b.client().Delete(InstanceRef{ID: fixtureID, ServiceID: "redis", PlanID: "redis-plan"}); err != nil {
		t.Fatal(err)
	}
	if got := b.requests()[0].Path; got != "/v2/service_instances/"+fixtureID+"?accepts_incomplete=true&plan_id=redis-plan&service_id=redis" {
		t.Errorf("path: %s", got)
	}
}
