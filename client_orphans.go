package main

import "fmt"

// Orphan is an index entry the broker's reconciler marked orphaned.
type Orphan struct {
	InstanceID     string `json:"instance_id"`
	InstanceName   string `json:"instance_name"`
	DeploymentName string `json:"deployment_name"`
	OrphanedAt     string `json:"orphaned_at"`
	Reason         string `json:"reason"`
	CheckCount     int    `json:"check_count"`
	SecondsLeft    *int64 `json:"seconds_left"`
	Due            bool   `json:"due"`
	Basis          string `json:"basis"`
}

// Orphans is the answer to GET /b/orphans.
type Orphans struct {
	Orphans   []Orphan `json:"orphans"`
	Unclaimed []struct {
		InstanceID     string `json:"instance_id"`
		DeploymentName string `json:"deployment_name"`
		FirstSeen      string `json:"first_seen"`
	} `json:"unclaimed"`
	UnclaimedKnown      bool   `json:"unclaimed_known"`
	UnclaimedAsOf       string `json:"unclaimed_as_of"`
	RemovalAfterSeconds int64  `json:"removal_after_seconds"`
}

// Orphans reads the orphan report. Boss only reads it. The broker's
// cleanup route is not implemented, so Boss never calls it.
func (c Client) Orphans() (Orphans, error) {
	var out Orphans
	if _, err := c.request("GET", "/b/orphans", nil, &out); err != nil {
		return out, fmt.Errorf("failed to read the orphan report: %w", err)
	}
	return out, nil
}
