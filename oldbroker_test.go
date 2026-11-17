package main

import (
	"errors"
	"strings"
	"testing"
)

func TestAnOldBrokersPlainTextNotFoundNamesTheCommand(t *testing.T) {
	cases := []struct {
		name    string
		route   string
		fixture string
		run     func(r *runner) error
		command string
	}{
		{"tasks", "GET /b/tasks", "v182_endpoint_not_found.txt", func(r *runner) error { return r.tasksList(nil, TaskFilter{}) }, "boss tasks"},
		{"batch", "GET /b/upgrade/tasks", "v182_upgrade_endpoint_not_found.txt", func(r *runner) error { return r.batchList(nil) }, "boss batch"},
		{"orphans", "GET /b/orphans", "v182_orphans_not_found.txt", func(r *runner) error { return r.orphans(nil) }, "boss orphans"},
		{"monitor", "GET /b/bosh/vm-monitor", "v182_endpoint_not_found.txt", func(r *runner) error { return r.monitor(nil) }, "boss monitor"},
		{"certificates", "GET /b/certificates/inventory", "v182_endpoint_not_found.txt", func(r *runner) error { return r.certList(nil, CertFilter{}, false) }, "boss certificates"},
	}

	for _, tc := range cases {
		b := newBroker(t, nil)
		b.route(tc.route, func(int) reply {
			return reply{Status: 404, File: tc.fixture, ContentType: "text/plain; charset=utf-8"}
		})
		r, _, _ := testRunner(b.client(), "", true)

		err := tc.run(r)
		var old BrokerTooOldError
		if !errors.As(err, &old) {
			t.Errorf("%s: got %v, want a BrokerTooOldError", tc.name, err)
			continue
		}
		for _, want := range []string{tc.command, "too old"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: %q lacks %q", tc.name, err, want)
			}
		}
	}
}

func TestAJSONNotFoundIsNotTakenForAnOldBroker(t *testing.T) {
	b := newBroker(t, nil)
	b.status("GET /b/batch-missing", 404, "batch_missing.json")
	b.status("GET /b/upgrade/tasks/nope", 404, "batch_missing.json")
	r, _, _ := testRunner(b.client(), "", true)

	err := r.batchShow([]string{"nope"})
	var old BrokerTooOldError
	if err == nil || errors.As(err, &old) {
		t.Fatalf("got %v, want an ordinary not-found error", err)
	}
}

// The v1.8.2 broker answers these routes with the shapes the newer line
// uses, so the client reads each recorded answer without change.
func TestTheV182BrokerShapesReadForTheRemainingCommands(t *testing.T) {
	dep := "/b/deployments/" + fixtureDeployment
	b := newBroker(t, map[string]string{
		"GET /b/bosh/vm-monitor":         "v182_vm_monitor.json",
		"GET " + dep + "/errands":        "v182_errands.json",
		"POST " + dep + "/errands/smoke": "v182_errand_result.json",
		"POST " + dep + "/stop":          "v182_deployment_stop.json",
		"POST " + dep + "/start":         "v182_deployment_start.json",
		"GET /b/tasks/5102/output":       "v182_task_output.json",
		"POST /b/tasks/5103/cancel":      "v182_task_cancel.json",
	})
	c := b.client()

	if m, err := c.VMMonitor(); err != nil || !m.Running || m.ServiceCount != 1 {
		t.Errorf("monitor: %+v, %v", m, err)
	}
	if names, err := c.Errands(fixtureDeployment); err != nil || len(names) == 0 {
		t.Errorf("errands: %v, %v", names, err)
	}
	if res, err := c.RunErrand(fixtureDeployment, "smoke", nil); err != nil || res.ExitCode != 0 {
		t.Errorf("errand: %+v, %v", res, err)
	}
	for _, op := range []string{"stop", "start"} {
		if act, err := c.DeploymentOp(fixtureDeployment, op); err != nil || act.TaskID == 0 {
			t.Errorf("%s: %+v, %v", op, act, err)
		}
	}
	if out, err := c.TaskOutput(5102, ""); err != nil || out == "" {
		t.Errorf("task output: %q, %v", out, err)
	}
	if res, err := c.CancelTask(5103, false); err != nil || !res.Cancelled {
		t.Errorf("cancel: %+v, %v", res, err)
	}
}

func TestTheV182BrokerErrorShapesReachTheUser(t *testing.T) {
	b := newBroker(t, nil)
	b.status("POST /b/tasks/5102/cancel", 409, "v182_task_cancel_conflict.json")
	b.status("GET /b/tasks/9999/output", 404, "v182_task_output_missing.json")
	c := b.client()

	if _, err := c.CancelTask(5102, false); err == nil || !strings.Contains(err.Error(), "cannot be cancelled") {
		t.Errorf("cancel: %v", err)
	}
	if _, err := c.TaskOutput(9999, ""); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("output: %v", err)
	}
}
