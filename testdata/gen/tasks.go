//go:build ignore

package tasks_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"blacksmith/internal/bosh"
	"blacksmith/internal/handlers/tasks"
	"blacksmith/pkg/logger"
)

func TestZZFixtures(t *testing.T) {
	fd := newFakeDirector()
	t0 := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	end := t0.Add(95 * time.Second)
	fd.tasks = []bosh.Task{
		{ID: 5103, State: "processing", Description: "run errand smoke-tests from deployment fixture-dep", Timestamp: t0.Add(10 * time.Minute).Unix(), StartedAt: t0.Add(10 * time.Minute), User: "fixture-director-user", Deployment: "fixture-dep", ContextID: "fixture-context"},
		{ID: 5102, State: "done", Description: "create deployment 'fixture-dep'", Timestamp: t0.Unix(), StartedAt: t0, EndedAt: &end, Result: "/deployments/fixture-dep", User: "fixture-director-user", Deployment: "fixture-dep"},
		{ID: 5101, State: "error", Description: "create deployment 'fixture-other'", Timestamp: t0.Unix(), StartedAt: t0, EndedAt: &end, Result: "fixture failure text", User: "fixture-director-user", Deployment: "fixture-other"},
	}
	log, _ := logger.New(logger.Config{Level: "error", Format: "json"})
	h := tasks.NewHandler(tasks.Dependencies{Logger: log, Director: fd})

	serve := func(method, target, body string, fn func(w http.ResponseWriter, r *http.Request)) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		fn(rec, httptest.NewRequestWithContext(t.Context(), method, target, strings.NewReader(body)))
		return rec
	}

	rec := serve("GET", "/b/tasks?paged=1&type=recent&limit=50", "", h.ListTasks)
	zzSave(t, "bosh_tasks", "GET /b/tasks?paged=1", rec.Code, rec.Body.Bytes())
	rec = serve("GET", "/b/tasks?paged=1&type=recent&limit=2", "", h.ListTasks)
	zzSave(t, "bosh_tasks_more", "GET /b/tasks?paged=1&limit=2 (a third task exists)", rec.Code, rec.Body.Bytes())
	rec = serve("GET", "/b/tasks?paged=1&type=recent&offset=2000&limit=50", "", h.ListTasks)
	zzSave(t, "bosh_tasks_ceiling", "GET /b/tasks?paged=1&offset=2000 (past the listing depth)", rec.Code, rec.Body.Bytes())
	rec = serve("GET", "/b/tasks?type=recent", "", h.ListTasks)
	zzSave(t, "bosh_tasks_bare", "GET /b/tasks (no paged)", rec.Code, rec.Body.Bytes())

	fd.tasks = nil
	rec = serve("GET", "/b/tasks?paged=1", "", h.ListTasks)
	zzSave(t, "bosh_tasks_empty", "GET /b/tasks?paged=1 (none)", rec.Code, rec.Body.Bytes())

	fd.taskOutputs[outputKey{id: 5102, typ: "task"}] = "Using environment 'fixture-director'\nTask 5102 done\n"
	rec = serve("GET", "/b/tasks/5102/output", "", func(w http.ResponseWriter, r *http.Request) { h.GetTaskOutput(w, r, 5102) })
	zzSave(t, "task_output", "GET /b/tasks/5102/output", rec.Code, rec.Body.Bytes())

	fd.taskOutputs[outputKey{id: 5102, typ: "result"}] = "{\"time\":1793610000,\"stage\":\"Updating instance\",\"task\":\"fixture/0\",\"state\":\"started\",\"total\":1,\"index\":1,\"progress\":0}\n{\"time\":1793610030,\"stage\":\"Updating instance\",\"task\":\"fixture/0\",\"state\":\"finished\",\"total\":1,\"index\":1,\"progress\":100}"
	rec = serve("GET", "/b/tasks/5102/output?type=result", "", func(w http.ResponseWriter, r *http.Request) { h.GetTaskOutput(w, r, 5102) })
	zzSave(t, "task_output_events", "GET /b/tasks/5102/output?type=result", rec.Code, rec.Body.Bytes())

	rec = serve("GET", "/b/tasks/9999/output", "", func(w http.ResponseWriter, r *http.Request) { h.GetTaskOutput(w, r, 9999) })
	zzSave(t, "task_output_missing", "GET /b/tasks/9999/output", rec.Code, rec.Body.Bytes())

	fd.taskDetails = map[int]bosh.Task{}
	for _, tk := range []bosh.Task{
		{ID: 5103, State: "processing", Description: "run errand smoke-tests from deployment fixture-dep", Timestamp: t0.Add(10 * time.Minute).Unix(), StartedAt: t0.Add(10 * time.Minute), User: "fixture-director-user", Deployment: "fixture-dep", ContextID: "fixture-context"},
		{ID: 5102, State: "done", Description: "create deployment 'fixture-dep'", Timestamp: t0.Unix(), StartedAt: t0, EndedAt: &end, Result: "/deployments/fixture-dep", User: "fixture-director-user", Deployment: "fixture-dep"},
		{ID: 5101, State: "error", Description: "create deployment 'fixture-other'", Timestamp: t0.Unix(), StartedAt: t0, EndedAt: &end, Result: "fixture failure text", User: "fixture-director-user", Deployment: "fixture-other"},
		{ID: 5104, State: "queued", Description: "create deployment 'fixture-dep'", Timestamp: t0.Add(20 * time.Minute).Unix(), StartedAt: t0.Add(20 * time.Minute), User: "fixture-director-user", Deployment: "fixture-dep"},
	} {
		fd.taskDetails[tk.ID] = tk
	}
	for id, name := range map[int]string{5103: "task_detail_processing", 5102: "task_detail_done", 5101: "task_detail_error", 5104: "task_detail_queued"} {
		id := id
		rec = serve("GET", fmt.Sprintf("/b/tasks/%d", id), "", func(w http.ResponseWriter, r *http.Request) { h.GetTaskDetails(w, r, id) })
		zzSave(t, name, fmt.Sprintf("GET /b/tasks/%d", id), rec.Code, rec.Body.Bytes())
	}
	rec = serve("GET", "/b/tasks/9999", "", func(w http.ResponseWriter, r *http.Request) { h.GetTaskDetails(w, r, 9999) })
	zzSave(t, "task_detail_missing", "GET /b/tasks/9999", rec.Code, rec.Body.Bytes())

	rec = serve("POST", "/b/tasks/5103/cancel", "", func(w http.ResponseWriter, r *http.Request) { h.CancelTask(w, r, 5103) })
	zzSave(t, "task_cancel", "POST /b/tasks/5103/cancel", rec.Code, rec.Body.Bytes())
	rec = serve("POST", "/b/tasks/5103/cancel", `{"force":true}`, func(w http.ResponseWriter, r *http.Request) { h.CancelTask(w, r, 5103) })
	zzSave(t, "task_cancel_force", "POST /b/tasks/5103/cancel force", rec.Code, rec.Body.Bytes())
	fd.cancelTaskErr[5102] = bosh.ErrTaskCannotBeCancelled
	rec = serve("POST", "/b/tasks/5102/cancel", "", func(w http.ResponseWriter, r *http.Request) { h.CancelTask(w, r, 5102) })
	zzSave(t, "task_cancel_conflict", "POST /b/tasks/5102/cancel (finished)", rec.Code, rec.Body.Bytes())
}

func zzSave(t *testing.T, name, route string, code int, body []byte) {
	t.Helper()
	dir := os.Getenv("FIXOUT")
	if dir == "" {
		t.Skip("FIXOUT not set")
	}
	var out bytes.Buffer
	if err := json.Indent(&out, body, "", "  "); err != nil {
		out.Reset()
		out.Write(body)
		name += ".txt"
	} else {
		out.WriteString("\n")
		name += ".json"
	}
	if err := os.WriteFile(filepath.Join(dir, name), out.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(dir, "MANIFEST.tsv"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	fmt.Fprintf(f, "%s\t%d\t%s\n", name, code, route)
}
