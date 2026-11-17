package main

import (
	"bufio"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v2"
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
	// rawDeployment is a director deployment named with --deployment.
	rawDeployment string
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

		rawDeployment: opt.RawDeployment,
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
	_, _ = io.WriteString(r.out, asSent(fmt.Sprintf(format, args...)))
}

// eventKey identifies one task event, so a log that restarts with a new
// task shows the new events and never repeats the old ones.
func eventKey(e TaskEvent) string {
	return fmt.Sprintf("%d|%s|%s|%d|%s", e.Time.UnixNano(), e.Stage, e.Task, e.Index, e.State)
}

// pollFunc reads the real state of an operation. It answers done once the
// operation has ended, with an error when it ended badly. An error with
// done false means the state could not be read, which also stops a follow.
type pollFunc struct {
	read func() (done bool, err error)
	// what names the work for the message a timed-out follow prints.
	what string
}

// interruptedError is what Ctrl-C during a follow leaves behind. The work
// goes on at the broker, so the message says how to pick it up again.
type interruptedError struct{ id string }

func (e interruptedError) Error() string {
	return fmt.Sprintf("interrupted; the work is still running on the broker, so follow it again with: boss task --follow %s", e.id)
}

// followTimeoutError says a follow gave up waiting while the work was
// still going on at the broker.
type followTimeoutError struct {
	id, what string
	limit    time.Duration
}

func (e followTimeoutError) Error() string {
	return fmt.Sprintf("Boss stopped waiting after %s, but %s is still in progress on the broker, so follow it again with: boss task --follow %s", e.limit, e.what, e.id)
}

// followLimit is how long a follow may wait. The --timeout value applies
// when we give one, and the long default for calls that wait on BOSH
// applies otherwise.
func (r *runner) followLimit() time.Duration {
	if r.c.Timeout > 0 {
		return r.c.Timeout
	}
	return longTimeout
}

// exitCode is the process exit status for the result of a command.
func exitCode(err error) int {
	var in interruptedError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &in):
		return 130
	}
	return 1
}

// operationProgress follows an instance operation through the OSB
// last_operation answer, which is what create and update wait on.
func (r *runner) operationProgress(id string, ref OperationRef) pollFunc {
	return pollFunc{what: "the operation on instance " + id, read: func() (bool, error) {
		op, err := r.c.LastOperation(id, ref)
		switch {
		case errors.Is(err, ErrInstanceGone):
			return true, fmt.Errorf("instance %s is gone, so the operation cannot finish", id)
		case err != nil:
			return true, err
		}

		switch op.State {
		case "in progress":
			return false, nil
		case "succeeded":
			return true, nil
		case "failed":
			return true, fmt.Errorf("the operation failed: %s", shownReason(op.Description))
		}
		return true, fmt.Errorf("the broker reported the unknown state %q for the operation", shownReason(op.State))
	}}
}

// taskProgress follows one BOSH task through the broker's task route.
func (r *runner) taskProgress(taskID int) pollFunc {
	return pollFunc{what: fmt.Sprintf("BOSH task %d", taskID), read: func() (bool, error) {
		task, err := r.c.Task(taskID)
		if err != nil {
			return true, err
		}

		switch task.State {
		case "done":
			return true, nil
		case "error", "cancelled", "timeout":
			// This is director output for a task the operator asked to
			// follow, so it prints as sent.
			why := task.Error
			if why == "" {
				why = task.Result
			}
			if why != "" {
				why = ": " + asSent(why)
			}
			return true, fmt.Errorf("BOSH task %d ended as %s%s", taskID, task.State, why)
		}
		// queued, processing, cancelling, and any state not known yet
		return false, nil
	}}
}

// logBefore reads the task log as it stands before a command starts new
// work, so a follow can leave out the events of the task before it. The
// log belongs to the instance and not to one task, which means nothing in
// it says which task wrote an event. It returns nil when no follow is
// wanted or the log cannot be read.
func (r *runner) logBefore(id string, follow bool) []TaskEvent {
	if !follow {
		return nil
	}
	events, err := r.c.TaskEvents(id)
	if err != nil {
		return nil
	}
	return events
}

// follow prints the events of an instance's task log as they arrive and
// ends only when the operation itself has ended, which p decides from the
// broker's real state. The log is for display. It can lag behind, show an
// earlier finished task, or go quiet between stages, so it never ends the
// follow. shown holds the events already printed. Ctrl-C ends the follow
// with an interrupt error, because the operation is not finished.
func (r *runner) follow(id string, shown []TaskEvent, p pollFunc) error {
	seen := map[string]bool{}
	for _, e := range shown {
		seen[eventKey(e)] = true
	}

	show := func() {
		events, err := r.c.TaskEvents(id)
		if err != nil {
			return
		}
		for _, e := range events {
			if !seen[eventKey(e)] {
				seen[eventKey(e)] = true
				r.printf("%s\n", e)
			}
		}
	}

	limit := r.followLimit()
	deadline := time.Now().Add(limit)
	for {
		select {
		case <-r.ctx.Done():
			return interruptedError{id: id}
		case <-time.After(r.poll):
		}

		show()
		done, err := p.read()
		if r.ctx.Err() != nil {
			return interruptedError{id: id}
		}
		if done || err != nil {
			show()
			return err
		}
		if time.Now().After(deadline) {
			return followTimeoutError{id: id, what: p.what, limit: limit}
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

// task prints the deployment task log of an instance. With follow it then
// waits for the task the deployment is running now, which it finds through
// the broker's task list, and it fails when that task fails.
func (r *runner) task(args []string, follow bool) error {
	if len(args) != 1 {
		return usageErr("The `instance' argument is required.")
	}

	ref, err := r.c.Lookup(args[0])
	if err != nil {
		return err
	}
	id := ref.ID

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

	if !follow {
		return nil
	}

	taskID, err := r.currentTask(ref.DeploymentName)
	if err != nil {
		return err
	}
	if taskID == 0 {
		r.printf("No task is running for deployment %s.\n", ref.DeploymentName)
		return nil
	}

	r.printf("\nfollowing BOSH task %d...\n", taskID)
	return r.follow(id, events, r.taskProgress(taskID))
}

// currentTask finds the newest task that is queued or running on a
// deployment. It answers 0 when there is none.
func (r *runner) currentTask(deployment string) (int, error) {
	list, err := r.c.Tasks(TaskFilter{Type: "current"})
	if err != nil {
		return 0, err
	}

	newest := 0
	for _, t := range list.Tasks {
		if t.Deployment == deployment && t.ID > newest {
			newest = t.ID
		}
	}
	return newest, nil
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
func (r *runner) redeploy(args []string, restart, follow bool) error {
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

	before := r.logBefore(id, follow)
	res, err := r.c.Redeploy(id, restart)
	if err != nil {
		return err
	}

	r.printf("# %s\n", id)
	r.printf("%s of deployment %s started as BOSH task %d (%s).\n", oneOf(res.Operation, "restart", "recreate", "stop", "start"), res.Deployment, res.TaskID, oneOf(res.State, taskStates...))

	if !follow {
		r.printf("Follow it with: boss redeploy --follow %s, or boss task --follow %s\n", args[0], id)
		return nil
	}
	if res.TaskID == 0 {
		return errors.New("the broker gave no task to follow")
	}

	r.printf("\nfollowing BOSH task %d...\n", res.TaskID)
	return r.follow(id, before, r.taskProgress(res.TaskID))
}

// parseParams reads provisioning or update parameters. A value that
// starts with an opening brace is JSON. Anything else names a file that
// holds JSON or YAML.
func parseParams(value string) (map[string]interface{}, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}

	text := value
	if !strings.HasPrefix(strings.TrimSpace(value), "{") {
		b, err := os.ReadFile(strings.TrimPrefix(value, "@"))
		if err != nil {
			return nil, fmt.Errorf("cannot read parameters file: %w", err)
		}
		text = string(b)
	}

	// YAML is a superset of JSON, so one parser reads both.
	var raw map[interface{}]interface{}
	if err := yaml.Unmarshal([]byte(text), &raw); err != nil {
		return nil, fmt.Errorf("parameters are not valid JSON or YAML: %w", err)
	}

	norm, err := normalize("parameters", raw)
	if err != nil {
		return nil, err
	}
	out, ok := norm.(map[string]interface{})
	if !ok {
		return nil, errors.New("parameters must be an object")
	}
	return out, nil
}

// normalize turns the maps yaml.v2 reads into maps that encode as JSON.
// JSON keys are strings, so a key of any other type is refused, and the
// error names where it sits.
func normalize(path string, v interface{}) (interface{}, error) {
	switch t := v.(type) {
	case map[interface{}]interface{}:
		m := make(map[string]interface{}, len(t))
		for k, val := range t {
			key, ok := k.(string)
			if !ok {
				return nil, fmt.Errorf("%s key %v is not a string; quote it so it is read as text", path, k)
			}
			n, err := normalize(path+"."+key, val)
			if err != nil {
				return nil, err
			}
			m[key] = n
		}
		return m, nil
	case []interface{}:
		for i := range t {
			n, err := normalize(fmt.Sprintf("%s[%d]", path, i), t[i])
			if err != nil {
				return nil, err
			}
			t[i] = n
		}
	}
	return v, nil
}

// create provisions a service instance.
func (r *runner) create(args []string, id, paramValue string, follow bool) error {
	if len(args) != 1 {
		return usageErr("The `service/plan' argument is required.")
	}
	l := strings.SplitN(args[0], "/", 2)
	if len(l) != 2 {
		return usageErr("The argument must look like service/plan.")
	}

	params, err := parseParams(paramValue)
	if err != nil {
		return err
	}

	if id == "" {
		id = RandomName()
	}

	service, plan, err := r.c.Plan(l[0], l[1])
	if err != nil {
		return err
	}

	if err := r.confirm("Create instance %s of %s/%s in org %s and space %s?", id, l[0], l[1], r.c.org(), r.c.space()); err != nil {
		return err
	}

	created, err := r.c.Create(id, service.ID, plan.ID, params)
	if err != nil {
		return err
	}

	r.printf("%s/%s instance %s created.\n", l[0], l[1], id)
	if follow {
		r.printf("\ntailing deployment task log...\n")
		return r.follow(id, nil, r.operationProgress(id, OperationRef{Operation: created.Operation, ServiceID: service.ID, PlanID: plan.ID}))
	}
	return nil
}

// update changes the parameters or the plan of an instance.
func (r *runner) update(args []string, paramValue, planName string, follow bool) error {
	if len(args) != 1 {
		return usageErr("The `instance' argument is required.")
	}

	params, err := parseParams(paramValue)
	if err != nil {
		return err
	}
	if params == nil && planName == "" {
		return usageErr("Give --params, --plan, or both; there is nothing to update.")
	}

	ref, err := r.c.Lookup(args[0])
	if err != nil {
		return err
	}

	planID := ""
	if planName != "" {
		cat, err := r.c.Catalog()
		if err != nil {
			return err
		}
		planID, err = findPlan(cat, ref.ServiceID, ref.PlanID, planName)
		if err != nil {
			return err
		}
		if planID == ref.PlanID {
			return fmt.Errorf("instance %s already uses plan %s", ref.ID, planName)
		}
	}

	what := "parameters"
	if planName != "" {
		what = "plan " + planName
		if params != nil {
			what += " and parameters"
		}
	}
	if err := r.confirm("Update the %s of instance %s?", what, ref.ID); err != nil {
		return err
	}

	before := r.logBefore(ref.ID, follow)
	updated, err := r.c.Update(ref.ID, ref.ServiceID, planID, ref.PlanID, params)
	if err != nil {
		return err
	}

	r.printf("Service instance %s updating.\n", ref.ID)
	if follow {
		r.printf("\ntailing deployment task log...\n")
		return r.follow(ref.ID, before, r.operationProgress(ref.ID, OperationRef{Operation: updated.Operation, ServiceID: ref.ServiceID, PlanID: cmp.Or(planID, ref.PlanID)}))
	}
	return nil
}

// findPlan finds a plan of a service by name or ID. The plan the instance
// uses now decides whether a change is allowed. Its own plan_updateable
// value wins over the service's, and a missing value means false under
// the OSB specification.
func findPlan(cat Catalog, serviceID, currentPlanID, plan string) (string, error) {
	for _, s := range cat.Services {
		if s.ID != serviceID {
			continue
		}

		allowed := s.PlanUpdateable
		for _, p := range s.Plans {
			if p.ID == currentPlanID && p.PlanUpdateable != nil {
				allowed = p.PlanUpdateable
			}
		}
		switch {
		case allowed == nil:
			return "", fmt.Errorf("the catalog does not mark service '%s' plan_updateable, and the OSB specification treats a missing value as false, so a plan change is refused", s.Name)
		case !*allowed:
			return "", fmt.Errorf("the catalog says service '%s' does not allow a plan change (plan_updateable is false)", s.Name)
		}

		for _, p := range s.Plans {
			if p.ID == plan || p.Name == plan {
				return p.ID, nil
			}
		}
		return "", fmt.Errorf("service '%s' has no plan '%s'", s.Name, plan)
	}
	return "", fmt.Errorf("service '%s' is not in the catalog", serviceID)
}

// delete removes a service instance. The instance has to be in the
// broker's index, because the call needs its service and plan.
func (r *runner) delete(args []string) error {
	if len(args) != 1 {
		return usageErr("The `instance' argument is required.")
	}

	ref, err := r.c.Lookup(args[0])
	if err != nil {
		return err
	}

	if err := r.confirm("Delete instance %s (%s) and its BOSH deployment %s?", ref.ID, ref.InstanceName, ref.DeploymentName); err != nil {
		return err
	}

	if err := r.c.Delete(ref); err != nil {
		return err
	}

	r.printf("%s instance deleted.\n", ref.ID)
	return nil
}
