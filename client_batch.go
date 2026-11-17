package main

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// BatchSummary is one batch job in the list the broker keeps.
type BatchSummary struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Kind           string    `json:"kind"`
	Status         string    `json:"status"`
	Target         string    `json:"target"`
	TargetStemcell string    `json:"target_stemcell"`
	TargetReleases []string  `json:"target_releases"`
	TotalCount     int       `json:"total_count"`
	CompletedCount int       `json:"completed_count"`
	FailedCount    int       `json:"failed_count"`
	UnknownCount   int       `json:"unknown_count"`
	SkippedCount   int       `json:"skipped_count"`
	CancelledCount int       `json:"cancelled_count"`
	AwaitingCount  int       `json:"awaiting_decision_count"`
	CreatedAt      time.Time `json:"created_at"`
	CreatedBy      string    `json:"created_by"`
}

// BatchInstance is one instance of a batch job. Boss leaves out the
// fields that can hold command text.
type BatchInstance struct {
	InstanceID     string `json:"instance_id"`
	InstanceName   string `json:"instance_name"`
	DeploymentName string `json:"deployment_name"`
	Status         string `json:"status"`
	BOSHTaskID     int    `json:"bosh_task_id"`
	Error          string `json:"error"`
}

// BatchJob is the detail of one batch job. Its stemcell and release
// targets are objects here, in place of the strings of the list.
type BatchJob struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Kind           string `json:"kind"`
	Status         string `json:"status"`
	Paused         bool   `json:"paused"`
	PauseReason    string `json:"pause_reason"`
	TargetStemcell struct {
		OS      string `json:"os"`
		Version string `json:"version"`
	} `json:"target_stemcell"`
	TargetReleases []struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"target_releases"`
	Instances      []BatchInstance `json:"instances"`
	TotalCount     int             `json:"total_count"`
	CompletedCount int             `json:"completed_count"`
	FailedCount    int             `json:"failed_count"`
	UnknownCount   int             `json:"unknown_count"`
	SkippedCount   int             `json:"skipped_count"`
	CancelledCount int             `json:"cancelled_count"`
	CreatedAt      time.Time       `json:"created_at"`
	CreatedBy      string          `json:"created_by"`
}

// target describes what the job does, in a line.
func (j BatchJob) target() string {
	var parts []string
	if j.TargetStemcell.OS != "" {
		parts = append(parts, "stemcell "+j.TargetStemcell.OS+" "+j.TargetStemcell.Version)
	}
	for _, r := range j.TargetReleases {
		parts = append(parts, "release "+r.Name+" "+r.Version)
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, ", ")
}

// BatchTasks lists batch jobs with GET /b/upgrade/tasks.
func (c Client) BatchTasks() ([]BatchSummary, error) {
	var out []BatchSummary
	if _, err := c.request("GET", "/b/upgrade/tasks", nil, &out); err != nil {
		return nil, fmt.Errorf("failed to list batch jobs: %w", err)
	}
	return out, nil
}

func batchPath(id, suffix string) string {
	return "/b/upgrade/tasks/" + url.PathEscape(id) + suffix
}

// BatchTask reads one batch job. For a command job the broker asks for the
// command key, which the client sends when it has one.
func (c Client) BatchTask(id string) (BatchJob, error) {
	var out BatchJob
	if _, err := c.request("GET", batchPath(id, ""), nil, &out); err != nil {
		return out, fmt.Errorf("failed to read batch job %s: %w", id, err)
	}
	return out, nil
}

// BatchAction pauses, resumes, or cancels a batch job. action is pause,
// resume, or cancel, and the answer is the broker's message.
func (c Client) BatchAction(id, action string) (string, error) {
	var out struct {
		Message string `json:"message"`
	}
	if _, err := c.request("POST", batchPath(id, "/"+action), nil, &out); err != nil {
		return "", fmt.Errorf("failed to %s batch job %s: %w", action, id, err)
	}
	return out.Message, nil
}
