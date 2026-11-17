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
	for _, want := range []string{"4242", "processing", "create deployment", "run errand smoke-tests", "More tasks follow"} {
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
		"GET /b/tasks/4242/output": "task_output.json",
	})
	r, out, _ := testRunner(b.client(), "", true)

	if err := r.tasksOutput([]string{"4242"}, "debug"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "fixture debug line two") {
		t.Errorf("output: %s", out)
	}
	if got := b.requests()[0].Path; got != "/b/tasks/4242/output?type=debug" {
		t.Errorf("path: %s", got)
	}

	b.status("GET /b/tasks/4243/output", 200, "task_output_events.json")
	r, out, _ = testRunner(b.client(), "", true)
	if err := r.tasksOutput([]string{"4243"}, "result"); err != nil {
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
	b.status("POST /b/tasks/4242/cancel", 200, "task_cancel.json")

	r, _, _ := testRunner(b.client(), "n\n", false)
	if err := r.tasksCancel([]string{"4242"}, false); !errors.Is(err, errAborted) {
		t.Fatalf("got %v", err)
	}
	if len(b.requests()) != 0 {
		t.Error("a request went out after no")
	}

	r, out, _ := testRunner(b.client(), "y\n", false)
	if err := r.tasksCancel([]string{"4242"}, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "cancelled successfully") {
		t.Errorf("output: %s", out)
	}
}

func TestTasksCancelForceSendsTheFlag(t *testing.T) {
	b := newBroker(t, nil)
	b.status("POST /b/tasks/4242/cancel", 200, "task_cancel.json")
	r, _, _ := testRunner(b.client(), "", true)

	if err := r.tasksCancel([]string{"4242"}, true); err != nil {
		t.Fatal(err)
	}
	if got := b.requests()[0].Body; got != `{"force":true}` {
		t.Errorf("body: %s", got)
	}
}

func TestTasksCancelReportsAConflict(t *testing.T) {
	b := newBroker(t, nil)
	b.status("POST /b/tasks/4241/cancel", 409, "task_cancel_conflict.json")
	r, _, _ := testRunner(b.client(), "", true)

	err := r.tasksCancel([]string{"4241"}, false)
	if err == nil || !strings.Contains(err.Error(), "cannot be cancelled") {
		t.Fatalf("got %v", err)
	}
}
