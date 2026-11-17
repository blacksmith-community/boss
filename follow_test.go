package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// opState answers last_operation from a script of fixtures, one per poll,
// and keeps the last one. It counts the polls it served.
func opState(b *fakeBroker, files ...string) *int {
	var polls int
	b.route("GET /v2/service_instances/"+fixtureID+"/last_operation", func(n int) reply {
		polls = n
		i := n - 1
		if i >= len(files) {
			i = len(files) - 1
		}
		return reply{Status: 200, File: files[i]}
	})
	return &polls
}

func repeat(file string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = file
	}
	return out
}

// followBroker serves the recorded log, which shows a task that has long
// finished, whatever the broker is doing now.
func followBroker(t *testing.T) *fakeBroker {
	return newBroker(t, map[string]string{
		"GET /b/status":                     "status.json",
		"GET /b/" + fixtureID + "/task/log": "task_log.json",
	})
}

func TestFollowWaitsForTheOperationWhileTheLogShowsAnOldFinishedTask(t *testing.T) {
	// A new task can wait behind a deployment lock for a long time while
	// the log still shows the task before it, finished.
	b := followBroker(t)
	polls := opState(b, append(repeat("osb_last_operation_progress.json", 8), "osb_last_operation_succeeded.json")...)
	r, cancel := followRunner(t, b)
	defer cancel()

	if err := r.follow(fixtureID, nil, r.operationProgress(fixtureID, OperationRef{})); err != nil {
		t.Fatal(err)
	}
	if *polls != 9 {
		t.Errorf("the state was read %d times, want 9", *polls)
	}
}

func TestFollowSurvivesAGapBetweenStages(t *testing.T) {
	// The log goes quiet between stages for longer than any fixed number
	// of polls, and the operation is still running.
	b := followBroker(t)
	polls := opState(b, append(repeat("osb_last_operation_progress.json", 30), "osb_last_operation_succeeded.json")...)
	r, cancel := followRunner(t, b)
	defer cancel()

	if err := r.follow(fixtureID, nil, r.operationProgress(fixtureID, OperationRef{})); err != nil {
		t.Fatal(err)
	}
	if *polls != 31 {
		t.Errorf("the state was read %d times, want 31", *polls)
	}
}

func TestFollowFailsWhenTheOperationFailsEvenIfTheLogLooksFinished(t *testing.T) {
	b := followBroker(t)
	opState(b, "osb_last_operation_progress.json", "osb_last_operation_failed.json")
	r, cancel := followRunner(t, b)
	defer cancel()

	err := r.follow(fixtureID, nil, r.operationProgress(fixtureID, OperationRef{}))
	if err == nil || !strings.Contains(err.Error(), "fixture failure text") {
		t.Fatalf("got %v, want the operation's failure", err)
	}
}

func TestFollowFailsWhenTheInstanceIsGone(t *testing.T) {
	b := followBroker(t)
	b.status("GET /v2/service_instances/"+fixtureID+"/last_operation", 410, "osb_last_operation_gone.json")
	r, cancel := followRunner(t, b)
	defer cancel()

	err := r.follow(fixtureID, nil, r.operationProgress(fixtureID, OperationRef{}))
	if err == nil {
		t.Fatal("a gone instance is not a success")
	}
}

func TestFollowFailsWhenTheStateCannotBeRead(t *testing.T) {
	b := followBroker(t)
	b.status("GET /v2/service_instances/"+fixtureID+"/last_operation", 500, "action_unknown.json")
	r, cancel := followRunner(t, b)
	defer cancel()

	if err := r.follow(fixtureID, nil, r.operationProgress(fixtureID, OperationRef{})); err == nil {
		t.Fatal("an unreadable state must not end as a success")
	}
}

func TestFollowStillShowsTheLogAsDisplay(t *testing.T) {
	b := followBroker(t)
	opState(b, "osb_last_operation_progress.json", "osb_last_operation_succeeded.json")
	r, cancel := followRunner(t, b)
	defer cancel()
	out := &strings.Builder{}
	r.out = out

	if err := r.follow(fixtureID, nil, r.operationProgress(fixtureID, OperationRef{})); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "redis/0 started") {
		t.Errorf("the log did not show:\n%s", out)
	}
}

func TestFollowKeepsGoingWhenTheLogCannotBeRead(t *testing.T) {
	// The log is a display. A missing log never ends or fails a follow.
	b := newBroker(t, map[string]string{"GET /b/status": "status.json"})
	b.status("GET /b/"+fixtureID+"/task/log", 404, "task_log_missing_instance.json")
	opState(b, "osb_last_operation_progress.json", "osb_last_operation_succeeded.json")
	r, cancel := followRunner(t, b)
	defer cancel()

	if err := r.follow(fixtureID, nil, r.operationProgress(fixtureID, OperationRef{})); err != nil {
		t.Fatalf("got %v, want success from the state alone", err)
	}
}

func TestControlCDuringAFollowIsAnInterruptNotASuccess(t *testing.T) {
	b := followBroker(t)
	opState(b, "osb_last_operation_progress.json")
	r, cancel := followRunner(t, b)
	r.c.Ctx = r.ctx

	go func() {
		time.Sleep(60 * time.Millisecond)
		cancel()
	}()

	err := r.follow(fixtureID, nil, r.operationProgress(fixtureID, OperationRef{}))
	var in interruptedError
	if !errors.As(err, &in) {
		t.Fatalf("got %v, want an interrupt", err)
	}
	if code := exitCode(err); code != 130 {
		t.Errorf("exit code %d, want 130", code)
	}
	if !strings.Contains(err.Error(), "still running") || !strings.Contains(err.Error(), fixtureID) {
		t.Errorf("the message should say the work goes on and how to follow it: %v", err)
	}
}

func TestExitCodes(t *testing.T) {
	if got := exitCode(nil); got != 0 {
		t.Errorf("nil gives %d", got)
	}
	if got := exitCode(errors.New("x")); got != 1 {
		t.Errorf("an error gives %d", got)
	}
	if got := exitCode(fmt.Errorf("wrapped: %w", interruptedError{id: "x"})); got != 130 {
		t.Errorf("a wrapped interrupt gives %d", got)
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
	opState(b, repeat("osb_last_operation_progress.json", 2)[0], "osb_last_operation_succeeded.json")
	r, cancel := followRunner(t, b)
	defer cancel()
	out := &strings.Builder{}
	r.out = out

	// The first task is already shown, so only the second one prints.
	events, err := r.c.TaskEvents(fixtureID)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.follow(fixtureID, events, r.operationProgress(fixtureID, OperationRef{})); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "redis/1 started") || strings.Contains(out.String(), "redis/0") {
		t.Errorf("output:\n%s", out)
	}
}

// taskScript answers GET /b/tasks/{id} from a script of fixtures.
func taskScript(b *fakeBroker, id int, files ...string) *int {
	var polls int
	b.route(fmt.Sprintf("GET /b/tasks/%d", id), func(n int) reply {
		polls = n
		i := n - 1
		if i >= len(files) {
			i = len(files) - 1
		}
		return reply{Status: 200, File: files[i]}
	})
	return &polls
}

func TestFollowAQueuedTaskUntilItIsDone(t *testing.T) {
	b := followBroker(t)
	polls := taskScript(b, 5104, "task_detail_queued.json", "task_detail_queued.json", "task_detail_processing.json", "task_detail_done.json")
	r, cancel := followRunner(t, b)
	defer cancel()

	if err := r.follow(fixtureID, nil, r.taskProgress(5104)); err != nil {
		t.Fatal(err)
	}
	if *polls != 4 {
		t.Errorf("the task was read %d times, want 4", *polls)
	}
}

func TestFollowATaskThatEndsInErrorFails(t *testing.T) {
	b := followBroker(t)
	taskScript(b, 5104, "task_detail_processing.json", "task_detail_error.json")
	r, cancel := followRunner(t, b)
	defer cancel()

	err := r.follow(fixtureID, nil, r.taskProgress(5104))
	if err == nil || !strings.Contains(err.Error(), "fixture failure text") || !strings.Contains(err.Error(), "5104") {
		t.Fatalf("got %v, want the task's failure", err)
	}
}

func TestFollowATaskThatIsCancelledOrTimesOutFails(t *testing.T) {
	for _, state := range []string{"cancelled", "timeout"} {
		body := strings.Replace(fixture(t, "task_detail_done.json"), `"state": "done"`, `"state": "`+state+`"`, 1)
		b := followBroker(t)
		b.route("GET /b/tasks/5104", func(int) reply { return reply{Status: 200, Body: body} })
		r, cancel := followRunner(t, b)

		err := r.follow(fixtureID, nil, r.taskProgress(5104))
		cancel()
		if err == nil || !strings.Contains(err.Error(), state) {
			t.Errorf("%s: got %v, want a failure naming the state", state, err)
		}
	}
}

// currentTasks lists one running task for the fixture's deployment.
func currentTasks(id int) string {
	return fmt.Sprintf(`{"tasks":[{"id":%d,"state":"queued","description":"create deployment","deployment":"%s"},{"id":9000,"state":"processing","deployment":"some-other-deployment"}],"offset":0,"limit":50,"has_more":false,"exact":true}`, id, fixtureDeployment)
}

func TestTaskFollowFindsTheDeploymentsCurrentTaskAndWaitsForIt(t *testing.T) {
	b := newBroker(t, map[string]string{"GET /b/status": "status.json"})
	b.route("GET /b/"+fixtureID+"/task/log", func(n int) reply {
		if n == 1 {
			return reply{Status: 200, File: "task_log_first.json"}
		}
		return reply{Status: 200, File: "task_log.json"}
	})
	b.route("GET /b/tasks", func(int) reply { return reply{Status: 200, Body: currentTasks(5104)} })
	polls := taskScript(b, 5104, "task_detail_queued.json", "task_detail_queued.json", "task_detail_queued.json", "task_detail_done.json")
	r, out, _ := testRunner(b.client(), "", true)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	r.ctx = ctx

	if err := r.task([]string{fixtureID}, true); err != nil {
		t.Fatal(err)
	}
	if *polls != 4 {
		t.Errorf("the task was read %d times, want 4: the old finished log must not end the follow", *polls)
	}
	got := out.String()
	if n := strings.Count(got, "redis/0 started"); n != 1 {
		t.Errorf("first event printed %d times:\n%s", n, got)
	}
	if !strings.Contains(got, "redis/0 finished") {
		t.Errorf("a later event is missing:\n%s", got)
	}
}

func TestTaskFollowFailsWhenTheTaskFails(t *testing.T) {
	b := newBroker(t, map[string]string{"GET /b/status": "status.json", "GET /b/" + fixtureID + "/task/log": "task_log.json"})
	b.route("GET /b/tasks", func(int) reply { return reply{Status: 200, Body: currentTasks(5101)} })
	taskScript(b, 5101, "task_detail_processing.json", "task_detail_error.json")
	r, _, _ := testRunner(b.client(), "", true)

	err := r.task([]string{fixtureID}, true)
	if err == nil || !strings.Contains(err.Error(), "fixture failure text") {
		t.Fatalf("got %v, want the task's failure", err)
	}
}

func TestTaskFollowSaysWhenNothingIsRunning(t *testing.T) {
	b := newBroker(t, map[string]string{"GET /b/status": "status.json", "GET /b/" + fixtureID + "/task/log": "task_log.json"})
	b.route("GET /b/tasks", func(int) reply { return reply{Status: 200, Body: `{"tasks":[],"has_more":false,"exact":true}`} })
	r, out, _ := testRunner(b.client(), "", true)

	if err := r.task([]string{fixtureID}, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "No task is running") {
		t.Errorf("output:\n%s", out)
	}
}

func TestRedeployFollowPollsItsTask(t *testing.T) {
	b := newBroker(t, map[string]string{"GET /b/status": "status.json", "GET /b/" + fixtureID + "/task/log": "task_log.json"})
	b.status("POST /b/"+fixtureID+"/recreate", 202, "action_recreate.json")
	polls := taskScript(b, 4321, "task_detail_queued.json", "task_detail_processing.json", "task_detail_done.json")
	r, out, _ := testRunner(b.client(), "", true)

	if err := r.redeploy([]string{fixtureID}, false, true); err != nil {
		t.Fatal(err)
	}
	if *polls != 3 {
		t.Errorf("the task was read %d times, want 3", *polls)
	}
	if !strings.Contains(out.String(), "BOSH task 4321") {
		t.Errorf("output:\n%s", out)
	}
}

func TestRedeployWithoutFollowDoesNotPoll(t *testing.T) {
	b := newBroker(t, map[string]string{"GET /b/status": "status.json"})
	b.status("POST /b/"+fixtureID+"/recreate", 202, "action_recreate.json")
	r, out, _ := testRunner(b.client(), "", true)

	if err := r.redeploy([]string{fixtureID}, false, false); err != nil {
		t.Fatal(err)
	}
	if n := count(b.requests(), "GET", "/b/tasks"); n != 0 {
		t.Errorf("%d task reads without --follow", n)
	}
	if !strings.Contains(out.String(), "boss task --follow") {
		t.Errorf("output lacks the follow hint:\n%s", out)
	}
}

func TestRedeployFollowFailsWhenTheTaskFails(t *testing.T) {
	b := newBroker(t, map[string]string{"GET /b/status": "status.json", "GET /b/" + fixtureID + "/task/log": "task_log.json"})
	b.status("POST /b/"+fixtureID+"/recreate", 202, "action_recreate.json")
	taskScript(b, 4321, "task_detail_error.json")
	r, _, _ := testRunner(b.client(), "", true)

	if err := r.redeploy([]string{fixtureID}, false, true); err == nil {
		t.Fatal("a failed task must fail the command")
	}
}

func TestFollowStopsAtTheTimeoutAndSaysTheWorkIsStillRunning(t *testing.T) {
	b := followBroker(t)
	opState(b, "osb_last_operation_progress.json")
	r, cancel := followRunner(t, b)
	defer cancel()
	r.c.Timeout = 150 * time.Millisecond

	start := time.Now()
	err := r.follow(fixtureID, nil, r.operationProgress(fixtureID, OperationRef{}))
	if err == nil {
		t.Fatal("a follow that never finishes must end with an error")
	}
	if took := time.Since(start); took > time.Second {
		t.Errorf("the follow ran %s, past its 150ms limit", took)
	}
	for _, want := range []string{"still in progress", fixtureID, "boss task --follow"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q: %v", want, err)
		}
	}
	if exitCode(err) != 1 {
		t.Errorf("exit code %d, want 1 like other timeouts", exitCode(err))
	}
}

func TestATaskFollowNamesTheTaskWhenItTimesOut(t *testing.T) {
	b := followBroker(t)
	b.status("GET /b/tasks/5104", 200, "task_detail_processing.json")
	r, cancel := followRunner(t, b)
	defer cancel()
	r.c.Timeout = 150 * time.Millisecond

	err := r.follow(fixtureID, nil, r.taskProgress(5104))
	if err == nil || !strings.Contains(err.Error(), "BOSH task 5104") || !strings.Contains(err.Error(), "still in progress") {
		t.Errorf("got %v, want the task named as still in progress", err)
	}
}

func TestFollowWithNoTimeoutGivesItselfTheLongDefault(t *testing.T) {
	r, _, _ := testRunner(&Client{}, "", true)
	if got := r.followLimit(); got != longTimeout {
		t.Errorf("limit %s, want %s", got, longTimeout)
	}
	r.c.Timeout = 90 * time.Second
	if got := r.followLimit(); got != 90*time.Second {
		t.Errorf("limit %s, want the --timeout value", got)
	}
}
