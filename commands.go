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

	out, ok := normalize(raw).(map[string]interface{})
	if !ok {
		return nil, errors.New("parameters must be an object")
	}
	return out, nil
}

// normalize turns the maps yaml.v2 reads into maps that encode as JSON.
func normalize(v interface{}) interface{} {
	switch t := v.(type) {
	case map[interface{}]interface{}:
		m := make(map[string]interface{}, len(t))
		for k, val := range t {
			m[fmt.Sprint(k)] = normalize(val)
		}
		return m
	case []interface{}:
		for i := range t {
			t[i] = normalize(t[i])
		}
	}
	return v
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

	if _, err := r.c.Create(id, service.ID, plan.ID, params); err != nil {
		return err
	}

	r.printf("%s/%s instance %s created.\n", l[0], l[1], id)
	if follow {
		r.printf("\ntailing deployment task log...\n")
		return r.followEvents(id, 0)
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
		planID, err = findPlan(cat, ref.ServiceID, planName)
		if err != nil {
			return err
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

	if _, err := r.c.Update(ref.ID, ref.ServiceID, planID, ref.PlanID, params); err != nil {
		return err
	}

	r.printf("Service instance %s updating.\n", ref.ID)
	if follow {
		r.printf("\ntailing deployment task log...\n")
		return r.followEvents(ref.ID, 0)
	}
	return nil
}

// findPlan finds a plan of a service by name or ID.
func findPlan(cat Catalog, serviceID, plan string) (string, error) {
	for _, s := range cat.Services {
		if s.ID != serviceID {
			continue
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
