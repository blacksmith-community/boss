package main

import (
	"errors"
	"strings"
	"testing"
)

func TestTasksListPrintsATableAndPagingHint(t *testing.T) {
	b := newBroker(t, map[string]string{"GET /b/tasks": "bosh_tasks.json"})
	r, out, _ := testRunner(b.client(), "", true)

	if err := r.tasksList(nil, TaskFilter{Type: "current", State: "processing", Limit: 2, Offset: 4}); err != nil {
		t.Fatal(err)
	}

	got := out.String()
	for _, want := range []string{"5103", "processing", "create deployment", "run errand smoke-tests", "2026-11-02 09:10:00", "2026-11-02 09:00:00"} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %q:\n%s", want, got)
		}
	}
	path := b.requests()[0].Path
	for _, want := range []string{"paged=1", "type=current", "state=processing", "limit=2", "offset=4"} {
		if !strings.Contains(path, want) {
			t.Errorf("request %s lacks %s", path, want)
		}
	}
}

func TestTasksListHintsWhenMoreTasksFollow(t *testing.T) {
	b := newBroker(t, map[string]string{"GET /b/tasks": "bosh_tasks_more.json"})
	r, out, _ := testRunner(b.client(), "", true)

	if err := r.tasksList(nil, TaskFilter{Limit: 2}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "More tasks follow; raise --offset") {
		t.Errorf("output: %s", out)
	}
}

func TestTasksListSaysWhenTheListingHitsItsCeiling(t *testing.T) {
	b := newBroker(t, map[string]string{"GET /b/tasks": "bosh_tasks_ceiling.json"})
	r, out, _ := testRunner(b.client(), "", true)

	if err := r.tasksList(nil, TaskFilter{Offset: 2000}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if strings.Contains(got, "raise --offset") || !strings.Contains(got, "no deeper") {
		t.Errorf("a ceiling must not invite a deeper page:\n%s", got)
	}
}

// A broker older than the paging work answers the bare array whether or
// not paging was asked for, and the list still prints.
func TestTasksListReadsTheBareArrayOfAnOlderBroker(t *testing.T) {
	for _, file := range []string{"bosh_tasks_bare.json", "v182_tasks_paged_request.json"} {
		b := newBroker(t, map[string]string{"GET /b/tasks": file})
		r, out, _ := testRunner(b.client(), "", true)

		if err := r.tasksList(nil, TaskFilter{}); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		if !strings.Contains(out.String(), "5103") || strings.Contains(out.String(), "More tasks") {
			t.Errorf("%s output: %s", file, out)
		}
	}
}

func TestTasksListSaysWhenThereAreNone(t *testing.T) {
	b := newBroker(t, map[string]string{"GET /b/tasks": "bosh_tasks_empty.json"})
	r, out, _ := testRunner(b.client(), "", true)

	if err := r.tasksList(nil, TaskFilter{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "No BOSH tasks found.") {
		t.Errorf("output: %s", out)
	}
}

func TestTasksOutputPrintsTextAndEvents(t *testing.T) {
	b := newBroker(t, map[string]string{
		"GET /b/tasks/5102/output": "task_output.json",
	})
	r, out, _ := testRunner(b.client(), "", true)

	if err := r.tasksOutput([]string{"5102"}, "debug"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Task 5102 done") {
		t.Errorf("output: %s", out)
	}
	if got := b.requests()[0].Path; got != "/b/tasks/5102/output?type=debug" {
		t.Errorf("path: %s", got)
	}

	b.status("GET /b/tasks/5102/output", 200, "task_output_events.json")
	r, out, _ = testRunner(b.client(), "", true)
	if err := r.tasksOutput([]string{"5102"}, "result"); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(out.String(), "\n"); n != 2 {
		t.Errorf("want two event lines, got:\n%s", out)
	}
}

func TestTasksOutputRejectsANonNumber(t *testing.T) {
	r, _, _ := testRunner(&Client{}, "", true)

	var ue usageError
	if err := r.tasksOutput([]string{"abc"}, ""); !errors.As(err, &ue) {
		t.Errorf("got %v", err)
	}
}

func TestTasksCancelAsksThenCancels(t *testing.T) {
	b := newBroker(t, nil)
	b.status("POST /b/tasks/5103/cancel", 200, "task_cancel.json")

	r, _, _ := testRunner(b.client(), "n\n", false)
	if err := r.tasksCancel([]string{"5103"}, false); !errors.Is(err, errAborted) {
		t.Fatalf("got %v", err)
	}
	if len(b.requests()) != 0 {
		t.Error("a request went out after no")
	}

	r, out, _ := testRunner(b.client(), "y\n", false)
	if err := r.tasksCancel([]string{"5103"}, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "cancelled successfully") {
		t.Errorf("output: %s", out)
	}
}

func TestTasksCancelForceSendsTheFlag(t *testing.T) {
	b := newBroker(t, nil)
	b.status("POST /b/tasks/5103/cancel", 200, "task_cancel_force.json")
	r, _, _ := testRunner(b.client(), "", true)

	if err := r.tasksCancel([]string{"5103"}, true); err != nil {
		t.Fatal(err)
	}
	if got := b.requests()[0].Body; got != `{"force":true}` {
		t.Errorf("body: %s", got)
	}
}

func TestTasksCancelReportsAConflict(t *testing.T) {
	b := newBroker(t, nil)
	b.status("POST /b/tasks/5102/cancel", 409, "task_cancel_conflict.json")
	r, _, _ := testRunner(b.client(), "", true)

	err := r.tasksCancel([]string{"5102"}, false)
	if err == nil || !strings.Contains(err.Error(), "cannot be cancelled") {
		t.Fatalf("got %v", err)
	}
}
