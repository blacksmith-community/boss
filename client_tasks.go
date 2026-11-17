package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// BoshTask is one BOSH director task as /b/tasks reports it.
type BoshTask struct {
	ID          int    `json:"id"`
	State       string `json:"state"`
	Description string `json:"description"`
	Timestamp   int64  `json:"timestamp"`
	StartedAt   int64  `json:"started_at"`
	EndedAt     int64  `json:"ended_at"`
	Result      string `json:"result"`
	User        string `json:"user"`
	Deployment  string `json:"deployment"`
	ContextID   string `json:"context_id"`
}

// TaskFilter narrows a task listing.
type TaskFilter struct {
	Type   string // recent (default), current, or all
	State  string
	Limit  int
	Offset int
}

// TaskList is a page of tasks.
type TaskList struct {
	Tasks   []BoshTask `json:"tasks"`
	HasMore bool       `json:"has_more"`
}

// Tasks lists BOSH tasks with GET /b/tasks. It asks for the paged shape,
// which tells the caller whether more tasks follow.
func (c Client) Tasks(f TaskFilter) (TaskList, error) {
	q := url.Values{}
	q.Set("paged", "1")
	if f.Type != "" {
		q.Set("type", f.Type)
	}
	if f.State != "" {
		q.Set("state", f.State)
	}
	if f.Limit > 0 {
		q.Set("limit", strconv.Itoa(f.Limit))
	}
	if f.Offset > 0 {
		q.Set("offset", strconv.Itoa(f.Offset))
	}

	var out TaskList
	if _, err := c.request("GET", "/b/tasks?"+q.Encode(), nil, &out); err != nil {
		return out, fmt.Errorf("failed to list tasks: %w", err)
	}
	return out, nil
}

// TaskOutput reads the output of a BOSH task from
// /b/tasks/{id}/output. kind is task, debug, result, or event. The broker
// sends an event array for result and event, and text for the others.
func (c Client) TaskOutput(id int, kind string) (string, error) {
	path := fmt.Sprintf("/b/tasks/%d/output", id)
	if kind != "" {
		path += "?type=" + url.QueryEscape(kind)
	}

	_, b, err := c.rawRequest("GET", path, nil)
	if err != nil {
		return "", fmt.Errorf("failed to read output of task %d: %w", id, err)
	}
	return decodeTaskOutput(b)
}

// CancelResult is the broker's answer to a task cancel.
type CancelResult struct {
	TaskID    int    `json:"task_id"`
	Cancelled bool   `json:"cancelled"`
	Force     bool   `json:"force"`
	Message   string `json:"message"`
}

// CancelTask cancels a BOSH task with POST /b/tasks/{id}/cancel.
func (c Client) CancelTask(id int, force bool) (CancelResult, error) {
	var in interface{}
	if force {
		in = map[string]bool{"force": true}
	}

	var out CancelResult
	_, err := c.request("POST", fmt.Sprintf("/b/tasks/%d/cancel", id), in, &out)
	if err != nil {
		return out, fmt.Errorf("failed to cancel task %d: %w", id, err)
	}
	return out, nil
}

// decodeTaskOutput turns the output response into text. For a result or
// event request the broker returns an array of events, which prints as
// JSON lines.
func decodeTaskOutput(b []byte) (string, error) {
	var wrapped struct {
		Output string `json:"output"`
	}
	if err := json.Unmarshal(b, &wrapped); err == nil {
		return wrapped.Output, nil
	}

	var events []json.RawMessage
	if err := json.Unmarshal(b, &events); err != nil {
		return "", fmt.Errorf("failed to parse task output: %w", err)
	}

	var sb strings.Builder
	for _, e := range events {
		sb.Write(e)
		sb.WriteByte('\n')
	}
	return sb.String(), nil
}
