package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// errAborted is what a command returns when the operator declines to
// confirm a change.
var errAborted = errors.New("aborted; nothing was changed")

// usageError marks a command line the operator got wrong, so main can
// print the short hint in place of a failure.
type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

func usageErr(format string, args ...interface{}) error {
	return usageError{msg: fmt.Sprintf(format, args...)}
}

// runner carries what every command needs to run and to be tested.
type runner struct {
	c   *Client
	ctx context.Context
	in  *bufio.Reader
	out io.Writer
	err io.Writer
	// yes skips the confirmation of a change.
	yes bool
	// poll is how often a followed task is read again.
	poll time.Duration
}

func newRunner(c *Client, ctx context.Context) *runner {
	return &runner{
		c:    c,
		ctx:  ctx,
		in:   bufio.NewReader(os.Stdin),
		out:  os.Stdout,
		err:  os.Stderr,
		yes:  opt.Yes,
		poll: time.Second,
	}
}

// confirm asks the operator to approve a change. It answers nil when the
// operator passed --yes or types y or yes, and errAborted otherwise.
func (r *runner) confirm(format string, args ...interface{}) error {
	if r.yes {
		return nil
	}

	_, _ = fmt.Fprintf(r.err, "%s [y/N] ", fmt.Sprintf(format, args...))

	line, _ := r.in.ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return nil
	}

	_, _ = fmt.Fprintln(r.err)
	return errAborted
}

// printf writes plain text, with no color markup parsed from the data.
func (r *runner) printf(format string, args ...interface{}) {
	_, _ = fmt.Fprintf(r.out, format, args...)
}

// followEvents prints the events of an instance's task log as they
// arrive, until the context ends. printed counts the events already shown.
func (r *runner) followEvents(id string, printed int) error {
	for {
		select {
		case <-r.ctx.Done():
			return nil
		case <-time.After(r.poll):
		}

		events, err := r.c.TaskEvents(id)
		if err != nil {
			return err
		}

		for ; printed < len(events); printed++ {
			r.printf("%s\n", events[printed])
		}
	}
}

// log prints the Blacksmith broker's own log.
func (r *runner) log(args []string, o LogOptions) error {
	if len(args) != 0 {
		return usageErr("The log command takes no arguments.")
	}

	text, err := r.c.Log(o)
	if err != nil {
		return err
	}

	r.printf("%s\n", strings.TrimRight(text, "\n"))
	return nil
}

// task prints the deployment task log of an instance, and keeps printing
// when follow is set.
func (r *runner) task(args []string, follow bool) error {
	if len(args) != 1 {
		return usageErr("The `instance' argument is required.")
	}

	id, err := r.c.Resolve(args[0])
	if err != nil {
		return err
	}

	events, err := r.c.TaskEvents(id)
	if err != nil {
		return err
	}

	r.printf("# %s\n", id)
	if len(events) == 0 {
		r.printf("No task events yet.\n")
	}
	for _, e := range events {
		r.printf("%s\n", e)
	}

	if follow {
		return r.followEvents(id, len(events))
	}
	return nil
}

// manifest prints the BOSH deployment manifest of an instance.
func (r *runner) manifest(args []string) error {
	if len(args) != 1 {
		return usageErr("The `instance' argument is required.")
	}

	id, err := r.c.Resolve(args[0])
	if err != nil {
		return err
	}

	manifest, err := r.c.Manifest(id)
	if err != nil {
		return err
	}

	r.printf("# %s\n%s\n", id, strings.TrimRight(manifest, "\n"))
	return nil
}

// creds prints the credentials of an instance. It is the one command that
// shows them, and it never sends them to a log.
func (r *runner) creds(args []string) error {
	if len(args) != 1 {
		return usageErr("The `instance' argument is required.")
	}

	id, err := r.c.Resolve(args[0])
	if err != nil {
		return err
	}

	creds, err := r.c.Creds(id)
	if err != nil {
		return err
	}

	r.printf("# %s\n%s", id, creds)
	return nil
}

// redeploy recreates the VMs of an instance, or restarts their jobs.
func (r *runner) redeploy(args []string, restart bool) error {
	if len(args) != 1 {
		return usageErr("The `instance' argument is required.")
	}

	id, err := r.c.Resolve(args[0])
	if err != nil {
		return err
	}

	action := "recreate"
	if restart {
		action = "restart"
	}

	if err := r.confirm("%s the VMs of instance %s?", strings.ToUpper(action[:1])+action[1:], id); err != nil {
		return err
	}

	res, err := r.c.Redeploy(id, restart)
	if err != nil {
		return err
	}

	r.printf("# %s\n", id)
	r.printf("%s of deployment %s started as BOSH task %d (%s).\n", res.Operation, res.Deployment, res.TaskID, res.State)
	r.printf("Follow it with: boss task --follow %s\n", id)
	return nil
}
