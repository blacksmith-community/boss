package main

import (
	"fmt"
	"strconv"
	"time"
)

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// monitor prints the VM monitor summary.
func (r *runner) monitor(args []string) error {
	if len(args) != 0 {
		return usageErr("The monitor command takes no arguments.")
	}

	m, err := r.c.VMMonitor()
	if err != nil {
		return err
	}
	if !m.Running {
		reason := m.Error
		if reason == "" {
			reason = "it is not running"
		}
		r.printf("The VM monitor is not running: %s.\n", shownReason(reason))
		return nil
	}

	r.printf("The VM monitor is running; it checks %d services every %s, or every %s after a failure.\n",
		m.ServiceCount, m.NormalInterval, m.FailedInterval)
	r.printf("%d healthy, %d failing, %d provisioning, %d due for a check.\n\n",
		m.Summary.Healthy, m.Summary.Failing, m.Summary.Provisioning, m.Summary.DueForCheck)

	rows := make([][]string, 0, len(m.Services))
	for _, s := range m.Services {
		rows = append(rows, []string{s.ServiceID, s.DeploymentName, oneOf(s.LastStatus, vmStatuses...), yesNo(s.IsHealthy),
			strconv.Itoa(s.FailureCount), s.LastCheck.UTC().Format("2006-01-02 15:04:05")})
	}
	r.table([]string{"Instance", "Deployment", "Status", "Healthy", "Failures", "Last check"}, rows)
	return nil
}

// orphans prints the orphan report.
func (r *runner) orphans(args []string) error {
	if len(args) != 0 {
		return usageErr("The orphans command takes no arguments.")
	}

	o, err := r.c.Orphans()
	if err != nil {
		return err
	}

	if len(o.Orphans) == 0 {
		r.printf("No orphaned instances.\n")
	} else {
		rows := make([][]string, 0, len(o.Orphans))
		for _, e := range o.Orphans {
			left := "-"
			if e.SecondsLeft != nil {
				left = (time.Duration(*e.SecondsLeft) * time.Second).String()
			}
			if e.Due {
				left = "due now"
			}
			rows = append(rows, []string{e.InstanceID, e.DeploymentName, shownReason(e.Reason), strconv.Itoa(e.CheckCount), left})
		}
		r.table([]string{"Instance", "Deployment", "Reason", "Checks", "Removal in"}, rows)
		r.printf("\nThe broker removes an orphan after %s.\n", (time.Duration(o.RemovalAfterSeconds) * time.Second).String())
	}

	switch {
	case !o.UnclaimedKnown:
		r.printf("\nUnclaimed deployments are not known yet.\n")
	case len(o.Unclaimed) == 0:
		r.printf("\nNo unclaimed deployments as of %s.\n", o.UnclaimedAsOf)
	default:
		r.printf("\nUnclaimed deployments, which the broker leaves alone:\n")
		for _, u := range o.Unclaimed {
			r.printf("%s\n", fmt.Sprintf("  %s (first seen %s)", u.DeploymentName, u.FirstSeen))
		}
	}
	return nil
}
