package main

import (
	"errors"
	"strings"
	"testing"
)

func TestFollowFailsOnAnUnknownOSBState(t *testing.T) {
	body := strings.Replace(fixture(t, "osb_last_operation_progress.json"), "in progress", "somewhere else", 1)
	b := followBroker(t)
	b.route("GET /v2/service_instances/"+fixtureID+"/last_operation", func(int) reply { return reply{Status: 200, Body: body} })
	r, cancel := followRunner(t, b)
	defer cancel()

	err := r.follow(fixtureID, nil, r.operationProgress(fixtureID, OperationRef{}))
	if err == nil || !strings.Contains(err.Error(), "somewhere else") {
		t.Fatalf("got %v, want a failure that names the unknown state", err)
	}
}

func TestFollowShowsTheTaskErrorBeforeItsResult(t *testing.T) {
	body := fixture(t, "task_detail_error.json")
	body = strings.Replace(body, `"result": "fixture failure text"`, `"result": "fixture result text"`, 1)
	b := followBroker(t)
	b.route("GET /b/tasks/5104", func(int) reply { return reply{Status: 200, Body: body} })
	r, cancel := followRunner(t, b)
	defer cancel()

	err := r.follow(fixtureID, nil, r.taskProgress(5104))
	if err == nil || !strings.Contains(err.Error(), "fixture failure text") || strings.Contains(err.Error(), "fixture result text") {
		t.Fatalf("got %v, want the error field and not the result", err)
	}
}

func TestFollowFallsBackToTheTaskResultWhenThereIsNoError(t *testing.T) {
	body := strings.Replace(fixture(t, "task_detail_error.json"), `"error": "fixture failure text",`, "", 1)
	b := followBroker(t)
	b.route("GET /b/tasks/5104", func(int) reply { return reply{Status: 200, Body: body} })
	r, cancel := followRunner(t, b)
	defer cancel()

	err := r.follow(fixtureID, nil, r.taskProgress(5104))
	if err == nil || !strings.Contains(err.Error(), "fixture failure text") {
		t.Fatalf("got %v, want the result text", err)
	}
}

func TestTaskFollowChoosesTheNewestTaskOfTheDeployment(t *testing.T) {
	b := newBroker(t, map[string]string{"GET /b/status": "status.json", "GET /b/" + fixtureID + "/task/log": "task_log.json"})
	b.route("GET /b/tasks", func(int) reply {
		return reply{Status: 200, Body: `{"tasks":[{"id":5200,"state":"queued","deployment":"` + fixtureDeployment + `"},{"id":5100,"state":"processing","deployment":"` + fixtureDeployment + `"},{"id":9999,"state":"processing","deployment":"other-dep"}],"has_more":false,"exact":true}`}
	})
	newest := taskScript(b, 5200, "task_detail_done.json")
	oldest := taskScript(b, 5100, "task_detail_error.json")
	r, cancel := followRunner(t, b)
	defer cancel()

	if err := r.task([]string{fixtureID}, true); err != nil {
		t.Fatal(err)
	}
	if *newest == 0 || *oldest != 0 {
		t.Errorf("the newest task was read %d times and the oldest %d times", *newest, *oldest)
	}
}

func TestCreateFollowWaitsForTheOperationToEnd(t *testing.T) {
	b := osbBroker(t)
	polls := opState(b, "osb_last_operation_progress.json", "osb_last_operation_progress.json", "osb_last_operation_succeeded.json")
	b.route("GET /v2/service_instances/new-one/last_operation", func(n int) reply {
		*polls = n
		if n < 3 {
			return reply{Status: 200, File: "osb_last_operation_progress.json"}
		}
		return reply{Status: 200, File: "osb_last_operation_succeeded.json"}
	})
	r, cancel := followRunner(t, b)
	defer cancel()

	if err := r.create([]string{"redis/small"}, "new-one", "", true); err != nil {
		t.Fatal(err)
	}
	if *polls != 3 {
		t.Errorf("polled %d times, want 3", *polls)
	}
}

func TestCreateAndUpdateFollowFailWhenTheOperationFails(t *testing.T) {
	b := osbBroker(t)
	b.status("GET /v2/service_instances/new-one/last_operation", 200, "osb_last_operation_failed.json")
	b.status("GET /v2/service_instances/"+fixtureID+"/last_operation", 200, "osb_last_operation_failed.json")
	r, cancel := followRunner(t, b)
	defer cancel()

	if err := r.create([]string{"redis/small"}, "new-one", "", true); err == nil {
		t.Error("a failed create operation must fail the command")
	}
	if err := r.update([]string{fixtureID}, `{"k":"v"}`, "", true); err == nil {
		t.Error("a failed update operation must fail the command")
	}
}

func TestAGoneInstanceIsReportedAsGoneThroughLastOperation(t *testing.T) {
	b := newBroker(t, nil)
	b.status("GET /v2/service_instances/"+fixtureID+"/last_operation", 410, "osb_last_operation_gone.json")

	_, err := b.client().LastOperation(fixtureID, OperationRef{})
	if !errors.Is(err, ErrInstanceGone) {
		t.Fatalf("got %v, want ErrInstanceGone", err)
	}

	r, cancel := followRunner(t, b)
	defer cancel()
	err = r.follow(fixtureID, nil, r.operationProgress(fixtureID, OperationRef{}))
	if err == nil || !strings.Contains(err.Error(), "is gone") {
		t.Errorf("got %v, want the gone message", err)
	}
}

func TestFinishExitsWithTheCodeOfTheError(t *testing.T) {
	var code = -1
	old := exit
	exit = func(c int) { code = c }
	t.Cleanup(func() { exit = old })

	finish("create", interruptedError{id: "x"})
	if code != 130 {
		t.Errorf("exit code %d after Ctrl-C, want 130", code)
	}
	finish("create", errors.New("boom"))
	if code != 1 {
		t.Errorf("exit code %d after a failure, want 1", code)
	}
	code = -1
	finish("create", nil)
	if code != -1 {
		t.Errorf("a command that worked exited with %d", code)
	}
}
