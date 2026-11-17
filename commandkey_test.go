package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTheCommandKeyGoesOnlyToTheUpgradeRoutes(t *testing.T) {
	b := newBroker(t, map[string]string{
		"GET /b/status":        "status.json",
		"GET /b/tasks":         "bosh_tasks_bare.json",
		"GET /b/upgrade/tasks": "batch_list.json",
	})
	c := b.client()
	c.CommandKey = "fixture-command-key"

	if _, err := c.statusIndex(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Tasks(TaskFilter{}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.BatchTasks(); err != nil {
		t.Fatal(err)
	}

	for _, req := range b.requests() {
		want := ""
		if strings.HasPrefix(req.Path, "/b/upgrade/") {
			want = "fixture-command-key"
		}
		if req.CommandKey != want {
			t.Errorf("%s sent command key %q, want %q", req.Path, req.CommandKey, want)
		}
	}
}

func TestTheCommandKeyCanComeFromAFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(path, []byte("fixture-command-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := commandKey("", path)
	if err != nil || got != "fixture-command-key" {
		t.Fatalf("got %q, %v", got, err)
	}
	if got, _ := commandKey("typed-key", path); got != "typed-key" {
		t.Errorf("a key given directly lost to the file: %q", got)
	}
	if _, err := commandKey("", filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Error("a missing key file was not reported")
	}
}

func TestDebugOutputHidesPasswordInTheURL(t *testing.T) {
	b := newBroker(t, map[string]string{"GET /b/status": "status.json"})
	c := b.client()
	c.URL = strings.Replace(b.srv.URL, "http://", "http://url-user:fixture-url-password@", 1)
	var logged strings.Builder
	c.Debug, c.Stderr = true, &logged

	if _, err := c.statusIndex(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logged.String(), "fixture-url-password") {
		t.Errorf("the log shows the URL password:\n%s", logged.String())
	}
	if !strings.Contains(logged.String(), "REQUEST: GET") {
		t.Errorf("no request line in:\n%s", logged.String())
	}
}
