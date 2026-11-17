package main

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

// deployment names the BOSH deployment a command acts on. An explicit
// --deployment names it outright. Otherwise the first argument is an
// instance ID or name that the broker's index has to list, and any
// failure to look it up stops the command, so a typo can never reach
// some other deployment. It returns the arguments that are left.
func (r *runner) deployment(args []string, extra int, noun string) (string, []string, error) {
	if r.rawDeployment != "" {
		if len(args) != extra {
			return "", nil, usageErr("With --deployment, give %s and no instance.", noun)
		}
		return r.rawDeployment, args, nil
	}

	if len(args) != extra+1 {
		return "", nil, usageErr("The `instance' argument is required, or use --deployment NAME for a director deployment.")
	}

	ref, err := r.c.Lookup(args[0])
	if err != nil {
		if errors.Is(err, ErrNoInstance) {
			return "", nil, fmt.Errorf("%w; to act on a director deployment by its own name, pass --deployment %s", err, args[0])
		}
		return "", nil, err
	}
	if ref.DeploymentName == "" {
		return "", nil, fmt.Errorf("the broker lists no deployment for instance %s", ref.ID)
	}
	return ref.DeploymentName, args[1:], nil
}

var opWarnings = map[string]string{
	"stop":     "Stop every VM of deployment %s? Its service goes down until it is started again.",
	"restart":  "Restart the jobs on every VM of deployment %s?",
	"recreate": "Recreate every VM of deployment %s?",
	"start":    "Start the VMs of deployment %s?",
}

// deploymentOp restarts, recreates, stops, or starts a deployment.
func (r *runner) deploymentOp(args []string, op string) error {
	name, _, err := r.deployment(args, 0, "no arguments")
	if err != nil {
		return err
	}
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
	name, _, err := r.deployment(args, 0, "no arguments")
	if err != nil {
		return err
	}
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
	name, rest, err := r.deployment(args, 1, "the errand")
	if err != nil {
		return err
	}
	if strings.TrimSpace(rest[0]) == "" {
		return errors.New("the errand name is empty; give the name of an errand from boss deployment errands")
	}
	if err := r.confirm("Run errand %s on deployment %s?", rest[0], name); err != nil {
		return err
	}

	res, err := r.c.RunErrand(name, rest[0], instances)
	if err != nil {
		return err
	}

	if res.Skipped {
		r.printf("Errand %s was skipped.\n", res.Errand)
		return nil
	}

	r.printf("Errand %s exited with %d.\n", res.Errand, res.ExitCode)
	if res.Stdout != "" {
		_, _ = io.WriteString(r.out, asSent("\n--- stdout ---\n"+strings.TrimRight(res.Stdout, "\n")+"\n"))
	}
	if res.Stderr != "" {
		_, _ = io.WriteString(r.out, asSent("\n--- stderr ---\n"+strings.TrimRight(res.Stderr, "\n")+"\n"))
	}
	for _, e := range res.Results {
		r.printf("\n%s (%s) exited with %d.\n", e.Instance, e.InstanceID, e.ExitCode)
		if e.Stdout != "" {
			_, _ = io.WriteString(r.out, asSent(strings.TrimRight(e.Stdout, "\n")+"\n"))
		}
		if e.Stderr != "" {
			_, _ = io.WriteString(r.out, asSent(strings.TrimRight(e.Stderr, "\n")+"\n"))
		}
	}

	failed := res.ExitCode
	for _, e := range res.Results {
		if failed == 0 {
			failed = e.ExitCode
		}
	}
	if failed != 0 {
		return fmt.Errorf("errand %s exited with %d", res.Errand, failed)
	}
	return nil
}
