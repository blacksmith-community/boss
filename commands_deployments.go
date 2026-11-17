package main

import (
	"io"
	"strings"
)

// deploymentName turns an instance ID or prefix into the name of its
// BOSH deployment. Anything the broker's index does not list is taken as a
// deployment name.
func (r *runner) deploymentName(arg string) string {
	if ref, err := r.c.Lookup(arg); err == nil && ref.DeploymentName != "" {
		return ref.DeploymentName
	}
	return arg
}

var opWarnings = map[string]string{
	"stop":     "Stop every VM of deployment %s? Its service goes down until it is started again.",
	"restart":  "Restart the jobs on every VM of deployment %s?",
	"recreate": "Recreate every VM of deployment %s?",
	"start":    "Start the VMs of deployment %s?",
}

// deploymentOp restarts, recreates, stops, or starts a deployment.
func (r *runner) deploymentOp(args []string, op string) error {
	if len(args) != 1 {
		return usageErr("The `deployment' argument is required.")
	}

	name := r.deploymentName(args[0])
	if err := r.confirm(opWarnings[op], name); err != nil {
		return err
	}

	res, err := r.c.DeploymentOp(name, op)
	if err != nil {
		return err
	}

	r.printf("%s of deployment %s started as BOSH task %d (%s).\n", res.Operation, res.Deployment, res.TaskID, res.State)
	return nil
}

// errands lists the errands of a deployment.
func (r *runner) errands(args []string) error {
	if len(args) != 1 {
		return usageErr("The `deployment' argument is required.")
	}

	name := r.deploymentName(args[0])
	list, err := r.c.Errands(name)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		r.printf("Deployment %s has no errands.\n", name)
		return nil
	}

	r.printf("# %s\n", name)
	for _, e := range list {
		r.printf("%s\n", e)
	}
	return nil
}

// errand runs an errand and shows what it printed.
func (r *runner) errand(args, instances []string) error {
	if len(args) != 2 {
		return usageErr("The `deployment' and `errand' arguments are required.")
	}

	name := r.deploymentName(args[0])
	if err := r.confirm("Run errand %s on deployment %s?", args[1], name); err != nil {
		return err
	}

	res, err := r.c.RunErrand(name, args[1], instances)
	if err != nil {
		return err
	}

	if res.Skipped {
		r.printf("Errand %s was skipped.\n", res.Errand)
		return nil
	}

	r.printf("Errand %s exited with %d.\n", res.Errand, res.ExitCode)
	if res.Stdout != "" {
		_, _ = io.WriteString(r.out, "\n--- stdout ---\n"+strings.TrimRight(res.Stdout, "\n")+"\n")
	}
	if res.Stderr != "" {
		_, _ = io.WriteString(r.out, "\n--- stderr ---\n"+strings.TrimRight(res.Stderr, "\n")+"\n")
	}
	for _, e := range res.Results {
		r.printf("\n%s (%s) exited with %d.\n", e.Instance, e.InstanceID, e.ExitCode)
		if e.Stdout != "" {
			_, _ = io.WriteString(r.out, strings.TrimRight(e.Stdout, "\n")+"\n")
		}
		if e.Stderr != "" {
			_, _ = io.WriteString(r.out, strings.TrimRight(e.Stderr, "\n")+"\n")
		}
	}
	return nil
}
