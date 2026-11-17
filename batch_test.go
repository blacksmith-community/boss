package main

import (
	"errors"
	"strings"
	"testing"
)

func TestBatchListPrintsJobs(t *testing.T) {
	b := newBroker(t, map[string]string{"GET /b/upgrade/tasks": "batch_list.json"})
	r, out, _ := testRunner(b.client(), "", true)

	if err := r.batchList(nil); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"job-aaaa-0001", "fixture stemcell upgrade", "2/4", "job-aaaa-0002", "paused"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestBatchShowPrintsInstancesWithoutCommandText(t *testing.T) {
	b := newBroker(t, map[string]string{"GET /b/upgrade/tasks/job-aaaa-0001": "batch_job.json"})
	r, out, _ := testRunner(b.client(), "", true)

	if err := r.batchShow([]string{"job-aaaa-0001"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Job:     job-aaaa-0001", "Status:  running", "fixture deploy failure", "4301", "stemcell ubuntu-noble 1.2.3, release redis 9.9.9"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out.String(), "fixture-command-text-never-printed") {
		t.Error("command text was printed")
	}
}

func TestBatchActionsPostToTheirRoutes(t *testing.T) {
	for action, file := range map[string]string{"pause": "batch_paused.json", "resume": "batch_resumed.json", "cancel": "batch_cancelled.json"} {
		b := newBroker(t, nil)
		b.status("POST /b/upgrade/tasks/job-aaaa-0001/"+action, 200, file)
		r, out, _ := testRunner(b.client(), "", true)

		if err := r.batchAction([]string{"job-aaaa-0001"}, action); err != nil {
			t.Fatalf("%s: %v", action, err)
		}
		if !strings.Contains(out.String(), "task ") {
			t.Errorf("%s output: %s", action, out)
		}
		if posts := only(b.requests(), "POST"); len(posts) != 1 {
			t.Errorf("%s: %d posts", action, len(posts))
		}
	}
}

func TestBatchActionsAskFirst(t *testing.T) {
	b := newBroker(t, nil)
	b.status("POST /b/upgrade/tasks/job-aaaa-0001/cancel", 200, "batch_cancelled.json")
	r, _, errb := testRunner(b.client(), "\n", false)

	if err := r.batchAction([]string{"job-aaaa-0001"}, "cancel"); !errors.Is(err, errAborted) {
		t.Fatalf("got %v", err)
	}
	if len(b.requests()) != 0 {
		t.Error("a request went out")
	}
	if !strings.Contains(errb.String(), "Cancel batch job job-aaaa-0001?") {
		t.Errorf("prompt: %s", errb)
	}
}

func TestBatchActionReportsAConflict(t *testing.T) {
	b := newBroker(t, nil)
	b.status("POST /b/upgrade/tasks/job-aaaa-0001/pause", 409, "batch_conflict.json")
	r, _, _ := testRunner(b.client(), "", true)

	err := r.batchAction([]string{"job-aaaa-0001"}, "pause")
	if err == nil || !strings.Contains(err.Error(), "invalid_task_state") {
		t.Fatalf("got %v", err)
	}
}

func TestBatchResumeSendsTheCommandKeyWithoutLoggingIt(t *testing.T) {
	b := newBroker(t, nil)
	b.status("POST /b/upgrade/tasks/job-aaaa-0001/resume", 200, "batch_resumed.json")
	c := b.client()
	c.CommandKey = "fixture-command-key"
	var logged strings.Builder
	c.Debug, c.Trace, c.Stderr = true, true, &logged
	r, _, _ := testRunner(c, "", true)

	if err := r.batchAction([]string{"job-aaaa-0001"}, "resume"); err != nil {
		t.Fatal(err)
	}
	if got := b.requests()[0].CommandKey; got != "fixture-command-key" {
		t.Errorf("the broker saw key %q", got)
	}
	if strings.Contains(logged.String(), "fixture-command-key") {
		t.Errorf("the key leaked:\n%s", logged.String())
	}
}

func TestBatchResumeExplainsAMissingCommandKey(t *testing.T) {
	b := newBroker(t, nil)
	b.status("POST /b/upgrade/tasks/job-aaaa-0001/resume", 403, "batch_key_required.json")
	r, _, _ := testRunner(b.client(), "", true)

	err := r.batchAction([]string{"job-aaaa-0001"}, "resume")
	if err == nil || !strings.Contains(err.Error(), "command key") {
		t.Fatalf("got %v", err)
	}
}
