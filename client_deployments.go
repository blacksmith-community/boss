package main

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

func deploymentPath(name, suffix string) string {
	return "/b/deployments/" + url.PathEscape(name) + suffix
}

// DeploymentAction is the broker's answer to a deployment lifecycle call.
type DeploymentAction struct {
	Deployment string `json:"deployment"`
	TaskID     int    `json:"task_id"`
	Operation  string `json:"operation"`
	State      string `json:"state"`
}

// DeploymentOp restarts, recreates, stops, or starts every VM of a
// deployment. op is one of those four words.
func (c Client) DeploymentOp(name, op string) (DeploymentAction, error) {
	var out DeploymentAction
	if _, err := c.long().request("POST", deploymentPath(name, "/"+op), nil, &out); err != nil {
		return out, fmt.Errorf("failed to %s deployment %s: %w", op, name, err)
	}
	return out, nil
}

// Errands lists the errands of a deployment.
func (c Client) Errands(name string) ([]string, error) {
	var out struct {
		Errands []string `json:"errands"`
	}
	if _, err := c.request("GET", deploymentPath(name, "/errands"), nil, &out); err != nil {
		return nil, fmt.Errorf("failed to list errands of %s: %w", name, err)
	}
	return out.Errands, nil
}

// ErrandResult is what running an errand produced.
type ErrandResult struct {
	Deployment string `json:"deployment"`
	Errand     string `json:"errand"`
	ExitCode   int    `json:"exit_code"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	Skipped    bool   `json:"skipped"`
	Results    []struct {
		Instance   string `json:"instance"`
		InstanceID string `json:"instance_id"`
		ExitCode   int    `json:"exit_code"`
		Stdout     string `json:"stdout"`
		Stderr     string `json:"stderr"`
	} `json:"results"`
}

// RunErrand runs an errand on a deployment, on the named instances when
// there are any.
func (c Client) RunErrand(name, errand string, instances []string) (ErrandResult, error) {
	if strings.TrimSpace(errand) == "" {
		return ErrandResult{}, errors.New("the errand name is empty; give the name of an errand from boss deployment errands")
	}
	path := deploymentPath(name, "/errands/"+url.PathEscape(errand))
	if len(instances) > 0 {
		path += "?instances=" + url.QueryEscape(strings.Join(instances, ","))
	}

	var out ErrandResult
	if _, err := c.long().request("POST", path, nil, &out); err != nil {
		return out, fmt.Errorf("failed to run errand %s on %s: %w", errand, name, err)
	}
	return out, nil
}
