package main

import (
	"strings"
	"testing"
)

func TestDeploymentFlagIsRefusedWhereItIsNotUsed(t *testing.T) {
	used := []string{"deployment restart", "deployment recreate", "deployment stop", "deployment start", "deployment errands", "deployment errand"}
	for _, cmd := range used {
		if err := checkRawDeployment(cmd, "dep-one"); err != nil {
			t.Errorf("%s uses --deployment, got %v", cmd, err)
		}
	}

	unused := []string{"log", "create", "update", "delete", "task", "tasks list", "batch cancel", "certificates list",
		"resurrection on", "monitor", "manifest", "redeploy", "creds", "orphans"}
	for _, cmd := range unused {
		err := checkRawDeployment(cmd, "dep-one")
		if err == nil || !strings.Contains(err.Error(), "--deployment") || !strings.Contains(err.Error(), cmd) {
			t.Errorf("%s ignores --deployment, want a refusal that names both, got %v", cmd, err)
		}
	}
}

func TestNoDeploymentFlagIsNeverRefused(t *testing.T) {
	for _, cmd := range []string{"log", "delete", "deployment restart", ""} {
		if err := checkRawDeployment(cmd, ""); err != nil {
			t.Errorf("%s without the flag: %v", cmd, err)
		}
	}
}
