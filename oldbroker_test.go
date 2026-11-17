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
