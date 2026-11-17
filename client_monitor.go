package main

import (
	"fmt"
	"time"
)

// VMMonitor is the broker's VM monitor status.
type VMMonitor struct {
	Running        bool   `json:"running"`
	Error          string `json:"error"`
	ServiceCount   int    `json:"service_count"`
	NormalInterval string `json:"normal_interval"`
	FailedInterval string `json:"failed_interval"`
	Services       []struct {
		ServiceID      string    `json:"service_id"`
		DeploymentName string    `json:"deployment_name"`
		LastStatus     string    `json:"last_status"`
		LastCheck      time.Time `json:"last_check"`
		NextCheck      time.Time `json:"next_check"`
		IsDue          bool      `json:"is_due"`
		FailureCount   int       `json:"failure_count"`
		IsHealthy      bool      `json:"is_healthy"`
		IsProvisioning bool      `json:"is_provisioning"`
	} `json:"services"`
	Summary struct {
		Total        int `json:"total"`
		DueForCheck  int `json:"due_for_check"`
		Healthy      int `json:"healthy"`
		Failing      int `json:"failing"`
		Provisioning int `json:"provisioning"`
	} `json:"summary"`
}

// VMMonitor reads GET /b/bosh/vm-monitor.
func (c Client) VMMonitor() (VMMonitor, error) {
	var out VMMonitor
	if _, err := c.request("GET", "/b/bosh/vm-monitor", nil, &out); err != nil {
		return out, fmt.Errorf("failed to read the VM monitor: %w", err)
	}
	return out, nil
}
