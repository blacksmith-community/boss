package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// logWith reads the recorded task log and lets a test change it, so one
// real response can stand for a task that fails or a second task.
func logWith(t *testing.T, change func([]map[string]interface{})) string {
	t.Helper()

	var events []map[string]interface{}
	if err := json.Unmarshal([]byte(fixture(t, "task_log.json")), &events); err != nil {
		t.Fatal(err)
	}
	change(events)

	out, err := json.Marshal(events)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func followRunner(t *testing.T, b *fakeBroker) (*runner, func()) {
	r, _, _ := testRunner(b.client(), "", true)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	r.ctx = ctx
	return r, cancel
}

func TestFollowStopsOnceTheTaskHasSettled(t *testing.T) {
	b := newBroker(t, map[string]string{"GET /b/status": "status.json", "GET /b/" + fixtureID + "/task/log": "task_log.json"})
	r, cancel := followRunner(t, b)
	defer cancel()

	started := time.Now()
	if err := r.followEvents(fixtureID, nil); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) > time.Second {
		t.Error("follow kept polling after every step finished")
	}
}

func TestFollowStopsAndFailsOnAFailedStep(t *testing.T) {
	failed := logWith(t, func(e []map[string]interface{}) {
		e[1]["state"] = "failed"
		e[1]["error"] = map[string]interface{}{"code": 100, "message": "fixture failure"}
	})
	b := newBroker(t, map[string]string{"GET /b/status": "status.json"})
	b.route("GET /b/"+fixtureID+"/task/log", func(int) reply { return reply{Status: 200, Body: failed} })
	r, cancel := followRunner(t, b)
	defer cancel()

	err := r.followEvents(fixtureID, nil)
	if err == nil || !strings.Contains(err.Error(), "failed") {
		t.Fatalf("got %v, want a failure", err)
	}
}

func TestFollowShowsTheEventsOfANewTask(t *testing.T) {
	later := logWith(t, func(e []map[string]interface{}) {
		for _, ev := range e {
			ev["time"] = strings.Replace(ev["time"].(string), "2026-11-14", "2026-11-15", 1)
			ev["task"] = "redis/1"
		}
	})
	b := newBroker(t, map[string]string{"GET /b/status": "status.json"})
	b.route("GET /b/"+fixtureID+"/task/log", func(n int) reply {
		if n == 1 {
			return reply{Status: 200, File: "task_log.json"}
		}
		return reply{Status: 200, Body: later}
	})
	r, cancel := followRunner(t, b)
	defer cancel()
	out := &strings.Builder{}
	r.out = out

	// The first task is already shown, so only the second one prints.
	events, err := r.c.TaskEvents(fixtureID)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.followEvents(fixtureID, events); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "redis/1 started") || strings.Contains(out.String(), "redis/0") {
		t.Errorf("output:\n%s", out)
	}
}
