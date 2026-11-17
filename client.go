package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v2"
)

// APIError represents an error from the blacksmith API
type APIError struct {
	Status      int    `json:"-"`
	Code        string `json:"error"`
	Description string `json:"description"`
	ErrorCode   string `json:"error_code,omitempty"`
	// BrokerCode is the short machine code the /b/ routes send, such as
	// deployment_locked. Their error field holds the message.
	BrokerCode string `json:"code,omitempty"`
}

func (e APIError) Error() string {
	msg := machineOrReason(e.Code)
	if e.Description != "" {
		msg = fmt.Sprintf("%s: %s", machineOrReason(e.Code), shownReason(e.Description))
	}
	if e.BrokerCode != "" {
		msg += " (" + e.BrokerCode + ")"
	}
	if e.Status == 504 {
		msg += "; the outcome is unknown and the action may still be running, so check boss tasks list before running it again"
	}
	return msg
}

// IsNotFound returns true if the error indicates a resource was not found
func IsNotFound(err error) bool {
	if apiErr, ok := err.(APIError); ok {
		return apiErr.Status == 404 || apiErr.Code == "NotFound"
	}
	return false
}

// IsConflict returns true if the error indicates a conflict
func IsConflict(err error) bool {
	if apiErr, ok := err.(APIError); ok {
		return apiErr.Status == 409 || apiErr.Code == "Conflict"
	}
	return false
}

// IsTimeout returns true if the error indicates a timeout
func IsTimeout(err error) bool {
	if apiErr, ok := err.(APIError); ok {
		return apiErr.Status == 408 || strings.Contains(apiErr.Description, "timeout")
	}
	return strings.Contains(err.Error(), "timeout")
}

// Client represents a connection to a Blacksmith service broker
type Client struct {
	// URL is the base URL of the Blacksmith service broker
	URL string
	// Username for basic authentication
	Username string
	// Password for basic authentication
	Password string
	// InsecureSkipVerify skips TLS certificate verification
	// WARNING: Setting this to true makes TLS connections vulnerable to man-in-the-middle attacks.
	// Only use this in development environments or when connecting to services with self-signed certificates.
	InsecureSkipVerify bool
	// Debug enables debug output to stderr
	Debug bool
	// Trace enables HTTP request/response tracing
	Trace bool
	// Timeout sets the HTTP client timeout for every call (default: 30s for
	// a read, 15m for a call that waits on BOSH)
	Timeout time.Duration
	// MaxRetries sets the maximum number of retry attempts (default: 3)
	MaxRetries int
	// BrokerAPIVersion sets the X-Broker-API-Version header (default: 2.16)
	BrokerAPIVersion string
	// Org is the organization_guid sent when provisioning (default: boss)
	Org string
	// Space is the space_guid sent when provisioning (default: boss)
	Space string
	// Stderr receives debug and trace output (default: os.Stderr)
	Stderr io.Writer
	// longCall marks a call that waits on BOSH, which gets a longer timeout
	longCall bool
	// Backoff is the unit of the pause between attempts (default: 1s)
	Backoff time.Duration
	// Ctx ends any request in flight when it is done (default: never)
	Ctx context.Context
	// RoundTripper, when set, carries every request in place of the shared
	// transport. Tests use it to watch or script what goes out.
	RoundTripper http.RoundTripper
	// CommandKey is the second credential a broker asks for before it
	// shows or resumes a batch command job
	CommandKey string
}

func (c Client) context() context.Context {
	if c.Ctx == nil {
		return context.Background()
	}
	return c.Ctx
}

func (c Client) stderr() io.Writer {
	if c.Stderr == nil {
		return os.Stderr
	}
	return c.Stderr
}

// defaultOrgSpace is the organization and space Boss has always sent.
const defaultOrgSpace = "boss"

func (c Client) org() string {
	if c.Org == "" {
		return defaultOrgSpace
	}
	return c.Org
}

func (c Client) space() string {
	if c.Space == "" {
		return defaultOrgSpace
	}
	return c.Space
}

// instancePath builds /b/{id}{suffix} with the id escaped as one segment.
func instancePath(id, suffix string) string {
	return "/b/" + url.PathEscape(id) + suffix
}

// Plan represents a service plan in the Blacksmith catalog
type Plan struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Free        bool   `json:"free,omitempty"`
	Bindable    *bool  `json:"bindable,omitempty"`
	// PlanUpdateable, when set, overrides the service's value for
	// instances of this plan.
	PlanUpdateable *bool `json:"plan_updateable,omitempty"`
}

// Service represents a service in the Blacksmith catalog
type Service struct {
	ID             string                 `json:"id"`
	Name           string                 `json:"name"`
	Description    string                 `json:"description"`
	Bindable       bool                   `json:"bindable"`
	Tags           []string               `json:"tags"`
	PlanUpdateable *bool                  `json:"plan_updateable,omitempty"`
	Plans          []Plan                 `json:"plans"`
	Requires       []string               `json:"requires,omitempty"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
}

// Catalog represents the Blacksmith service catalog
type Catalog struct {
	Services []Service `json:"services"`
}

func (c Catalog) Plan(service, plan string) (*Service, *Plan, error) {
	for _, s := range c.Services {
		if s.ID == service {
			for _, p := range s.Plans {
				if p.ID == plan {
					return &s, &p, nil
				}
			}
		}
	}
	for _, s := range c.Services {
		if s.Name == service {
			for _, p := range s.Plans {
				if p.Name == plan {
					return &s, &p, nil
				}
			}
		}
	}

	return nil, nil, fmt.Errorf("service '%s' / plan '%s' not found", service, plan)
}

// Instance represents a service instance
type Instance struct {
	ID      string   `json:"id"`
	Service *Service `json:"service"`
	Plan    *Plan    `json:"plan"`
	State   string   `json:"state,omitempty"`
	// Operation is the token the broker gave for an asynchronous create or
	// update. It goes back on every last_operation poll.
	Operation string    `json:"operation,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

// Catalog retrieves the service catalog from the Blacksmith broker.
// It returns a Catalog struct containing all available services and plans.
// The endpoint requires X-Broker-API-Version header which is automatically added.
func (c Client) Catalog() (Catalog, error) {
	var out Catalog
	_, err := c.request("GET", "/v2/catalog", nil, &out)
	if err != nil {
		return out, fmt.Errorf("failed to get catalog: %w", err)
	}
	return out, nil
}

func (c Client) Plan(service, plan string) (*Service, *Plan, error) {
	cat, err := c.Catalog()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get catalog: %w", err)
	}

	return cat.Plan(service, plan)
}

// InstanceRef is what /b/status records about one service instance.
type InstanceRef struct {
	ID             string
	ServiceID      string
	PlanID         string
	DeploymentName string
	InstanceName   string
}

// statusEntry is one instance of the /b/status index. The broker adds more
// fields than Boss reads.
type statusEntry struct {
	ServiceID      string      `json:"service_id"`
	PlanID         string      `json:"plan_id"`
	DeploymentName string      `json:"deployment_name"`
	InstanceName   string      `json:"instance_name"`
	State          string      `json:"state"`
	Status         string      `json:"status"`
	CreatedAt      string      `json:"created_at"`
	Created        json.Number `json:"created"`
	UpdatedAt      string      `json:"updated_at"`
}

func (e statusEntry) state() string {
	if e.State != "" {
		return e.State
	}
	return e.Status
}

func (e statusEntry) createdTime() time.Time {
	if t, err := time.Parse(time.RFC3339, e.CreatedAt); err == nil {
		return t
	}
	if n, err := e.Created.Int64(); err == nil && n > 0 {
		return time.Unix(n, 0).UTC()
	}
	return time.Time{}
}

func (e statusEntry) updatedTime() time.Time {
	if t, err := time.Parse(time.RFC3339, e.UpdatedAt); err == nil {
		return t
	}
	return time.Time{}
}

// statusIndex fetches the instance index from /b/status.
func (c Client) statusIndex() (map[string]statusEntry, error) {
	var out struct {
		Instances map[string]statusEntry `json:"instances"`
	}
	if _, err := c.request("GET", "/b/status", nil, &out); err != nil {
		return nil, fmt.Errorf("failed to get status: %w", err)
	}
	return out.Instances, nil
}

// ErrNoInstance is what Lookup wraps when the broker's index has no
// instance with the ID or name it was given.
var ErrNoInstance = errors.New("no instance has the ID or name")

// Lookup finds an instance by its exact ID or its exact name. It never
// matches a prefix or a substring, because the commands that change an
// instance act on whatever it returns.
func (c Client) Lookup(want string) (InstanceRef, error) {
	if strings.TrimSpace(want) == "" {
		return InstanceRef{}, errors.New("an instance ID or name is required")
	}

	index, err := c.statusIndex()
	if err != nil {
		return InstanceRef{}, err
	}

	pick := func(id string, e statusEntry) InstanceRef {
		return InstanceRef{ID: id, ServiceID: e.ServiceID, PlanID: e.PlanID,
			DeploymentName: e.DeploymentName, InstanceName: e.InstanceName}
	}

	if e, ok := index[want]; ok {
		return pick(want, e), nil
	}

	var matches []string
	for id, e := range index {
		if e.InstanceName == want {
			matches = append(matches, id)
		}
	}
	sort.Strings(matches)

	switch len(matches) {
	case 0:
		return InstanceRef{}, fmt.Errorf("%w '%s'", ErrNoInstance, want)
	case 1:
		return pick(matches[0], index[matches[0]]), nil
	default:
		return InstanceRef{}, fmt.Errorf("'%s' matches %d instances (%s); use the ID",
			want, len(matches), strings.Join(matches, ", "))
	}
}

// Resolve turns an instance ID or name into the instance ID.
func (c Client) Resolve(want string) (string, error) {
	ref, err := c.Lookup(want)
	if err != nil {
		return "", err
	}
	return ref.ID, nil
}

// LogOptions picks which broker log file to read and how much of it.
type LogOptions struct {
	File   string
	Limit  int
	Offset int
}

// Log reads the Blacksmith broker's own log from /b/blacksmith/logs.
func (c Client) Log(opts LogOptions) (string, error) {
	q := url.Values{}
	if opts.File != "" {
		q.Set("file", opts.File)
	}
	if opts.Limit > 0 {
		q.Set("limit", fmt.Sprint(opts.Limit))
	}
	if opts.Offset > 0 {
		q.Set("offset", fmt.Sprint(opts.Offset))
	}

	path := "/b/blacksmith/logs"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}

	var out struct {
		Logs  string      `json:"logs"`
		Error interface{} `json:"error"`
	}
	if _, err := c.request("GET", path, nil, &out); err != nil {
		return "", fmt.Errorf("failed to get log: %w", err)
	}
	if msg, ok := out.Error.(string); ok && msg != "" {
		return "", fmt.Errorf("failed to get log: %s", shownReason(msg))
	}
	return out.Logs, nil
}

// Instances retrieves all service instances from the Blacksmith broker.
// Returns a slice of Instance structs sorted by creation time (newest first).
// Unknown services/plans will be logged as warnings in debug mode.
func (c Client) Instances() ([]Instance, error) {
	cat, err := c.Catalog()
	if err != nil {
		return nil, fmt.Errorf("failed to get catalog: %w", err)
	}

	index, err := c.statusIndex()
	if err != nil {
		return nil, fmt.Errorf("failed to get instance status: %w", err)
	}

	instances := make([]Instance, 0, len(index))
	for id, info := range index {
		service, plan, _ := cat.Plan(info.ServiceID, info.PlanID)

		instance := Instance{
			ID:        id,
			State:     info.state(),
			CreatedAt: info.createdTime(),
			UpdatedAt: info.updatedTime(),
		}

		if service != nil && plan != nil {
			instance.Service = service
			instance.Plan = plan
		} else {
			// Log warning for unknown service/plan
			if c.Debug {
				_, _ = fmt.Fprintf(c.stderr(), "WARNING: Unknown service/plan for instance %s: %s/%s\n",
					id, info.ServiceID, info.PlanID)
			}
		}

		instances = append(instances, instance)
	}

	// Sort instances by creation time (newest first)
	sort.Slice(instances, func(i, j int) bool {
		// Handle zero time
		if instances[i].CreatedAt.IsZero() && instances[j].CreatedAt.IsZero() {
			return instances[i].ID < instances[j].ID // Sort by ID if no timestamp
		}
		if instances[i].CreatedAt.IsZero() {
			return false // Zero time comes last
		}
		if instances[j].CreatedAt.IsZero() {
			return true
		}
		return instances[i].CreatedAt.After(instances[j].CreatedAt)
	})

	return instances, nil
}

// provisionBody builds the OSB provision request.
func (c Client) provisionBody(service, plan string, params map[string]interface{}) interface{} {
	return struct {
		ServiceID  string                 `json:"service_id"`
		PlanID     string                 `json:"plan_id"`
		OrgID      string                 `json:"organization_guid"`
		SpaceID    string                 `json:"space_guid"`
		Parameters map[string]interface{} `json:"parameters,omitempty"`
		Context    map[string]interface{} `json:"context,omitempty"`
	}{
		ServiceID:  service,
		PlanID:     plan,
		OrgID:      c.org(),
		SpaceID:    c.space(),
		Parameters: params,
	}
}

// Create provisions a new service instance with the specified ID, service, plan, and parameters.
// This method supports asynchronous operations and will add the accepts_incomplete=true parameter.
// Returns an Instance struct with the ID, or an error if the operation fails.
func (c Client) Create(id, service, plan string, params map[string]interface{}) (Instance, error) {
	in := c.provisionBody(service, plan, params)

	_, body, err := c.rawRequest("PUT", "/v2/service_instances/"+url.PathEscape(id)+"?accepts_incomplete=true", in)
	if err != nil {
		return Instance{}, fmt.Errorf("failed to create instance %s: %w", id, err)
	}
	return Instance{ID: id, Operation: operationOf(body)}, nil
}

// Update modifies an existing service instance with new parameters or a
// new plan. When the plan changes, previousPlan names the plan the
// instance had, as the Open Service Broker API asks. This method supports
// asynchronous operations and adds accepts_incomplete=true.
func (c Client) Update(id, service, plan, previousPlan string, params map[string]interface{}) (Instance, error) {
	type previous struct {
		ServiceID string `json:"service_id,omitempty"`
		PlanID    string `json:"plan_id,omitempty"`
	}
	in := struct {
		ServiceID  string                 `json:"service_id"`
		PlanID     string                 `json:"plan_id,omitempty"`
		Parameters map[string]interface{} `json:"parameters,omitempty"`
		Previous   *previous              `json:"previous_values,omitempty"`
	}{
		ServiceID:  service,
		PlanID:     plan,
		Parameters: params,
	}
	if plan != "" && previousPlan != "" {
		in.Previous = &previous{ServiceID: service, PlanID: previousPlan}
	}

	_, body, err := c.rawRequest("PATCH", "/v2/service_instances/"+url.PathEscape(id)+"?accepts_incomplete=true", in)
	var apiErr APIError
	if errors.As(err, &apiErr) && apiErr.Status == 500 && strings.Contains(apiErr.Description, "not implemented") {
		return Instance{}, fmt.Errorf("failed to update instance %s: this broker doesn't support updates yet", id)
	}
	if err != nil {
		return Instance{}, fmt.Errorf("failed to update instance %s: %w", id, err)
	}
	return Instance{ID: id, Operation: operationOf(body)}, nil
}

// ErrInstanceGone is what Delete returns when the broker answers 410,
// which means it has no such instance.
var ErrInstanceGone = errors.New("the broker answered 410: instance not found, or already deleted")

// Delete removes a service instance. It sends the service and plan the
// broker listed for the instance, adds accepts_incomplete=true, and never
// sends the call again after a timeout.
func (c Client) Delete(ref InstanceRef) error {
	q := url.Values{}
	q.Set("accepts_incomplete", "true")
	q.Set("plan_id", ref.PlanID)
	q.Set("service_id", ref.ServiceID)

	_, err := c.long().request("DELETE", "/v2/service_instances/"+url.PathEscape(ref.ID)+"?"+q.Encode(), nil, nil)
	var api APIError
	if errors.As(err, &api) && api.Status == http.StatusGone {
		return fmt.Errorf("failed to delete instance %s: %w", ref.ID, ErrInstanceGone)
	}
	if err != nil {
		return fmt.Errorf("failed to delete instance %s: %w", ref.ID, err)
	}
	return nil
}

// TaskEvent is one line of a BOSH task as the broker reports it.
type TaskEvent struct {
	Time     time.Time              `json:"time"`
	Stage    string                 `json:"stage"`
	Tags     []string               `json:"tags"`
	Total    int                    `json:"total"`
	Task     string                 `json:"task"`
	Index    int                    `json:"index"`
	State    string                 `json:"state"`
	Progress int                    `json:"progress"`
	Data     map[string]interface{} `json:"data"`
	Error    *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// String renders the event as one line of text. The error is director output
// for a task the operator asked to see, so it prints as sent.
func (e TaskEvent) String() string {
	line := e.Time.UTC().Format("15:04:05")
	if e.Stage != "" {
		line += " | " + e.Stage
	}
	if e.Task != "" {
		line += ": " + e.Task
	}
	if e.State != "" {
		line += " " + e.State
	}
	if e.Error != nil {
		line += fmt.Sprintf(" (error %d: %s)", e.Error.Code, asSent(e.Error.Message))
	}
	return line
}

// TaskEvents retrieves the events of the latest deployment task of a
// service instance from /b/{id}/task/log.
func (c Client) TaskEvents(id string) ([]TaskEvent, error) {
	var events []TaskEvent
	_, err := c.request("GET", instancePath(id, "/task/log"), nil, &events)
	if err != nil {
		return nil, fmt.Errorf("failed to get task log for %s: %w", id, err)
	}
	return events, nil
}

// Manifest retrieves the BOSH deployment manifest for a service instance.
// The returned YAML is validated for syntax errors.
func (c Client) Manifest(id string) (string, error) {
	var out struct {
		Text  string `json:"text"`
		Error string `json:"error"`
	}
	_, err := c.request("GET", instancePath(id, "/manifest"), nil, &out)
	if err != nil {
		return "", fmt.Errorf("failed to get manifest for %s: %w", id, err)
	}
	if out.Text == "" {
		msg := out.Error
		if msg == "" {
			msg = "the broker returned no manifest"
		}
		return "", fmt.Errorf("failed to get manifest for %s: %s", id, shownReason(msg))
	}

	if err := c.validateYAML(out.Text); err != nil {
		return "", fmt.Errorf("invalid manifest for %s: %w", id, err)
	}

	return out.Text, nil
}

// validateYAML checks if the content is valid YAML
func (c Client) validateYAML(content string) error {
	var data interface{}
	if err := yaml.Unmarshal([]byte(content), &data); err != nil {
		return fmt.Errorf("invalid YAML: %w", err)
	}
	return nil
}

// Creds retrieves the service credentials of an instance from
// /b/{id}/credentials and returns them as YAML.
func (c Client) Creds(id string) (string, error) {
	var creds map[string]interface{}
	_, err := c.request("GET", instancePath(id, "/credentials"), nil, &creds)
	if err != nil {
		return "", fmt.Errorf("failed to get credentials for %s: %w", id, err)
	}

	// The broker answers 200 with only an error key when Vault fails.
	if msg, ok := creds["error"].(string); ok && len(creds) == 1 {
		return "", fmt.Errorf("failed to get credentials for %s: %s", id, msg)
	}

	// Brokers before the fix answer 200 with a stand-in when they hold no
	// credentials, and showing it would pass for the real thing.
	if creds["host"] == "10.0.0.1" && creds["username"] == "admin" && creds["password"] == "***" {
		return "", fmt.Errorf("the broker sent its placeholder credentials for %s, so it holds none for this instance", id)
	}

	b, err := yaml.Marshal(creds)
	if err != nil {
		return "", fmt.Errorf("failed to format credentials for %s: %w", id, err)
	}
	return string(b), nil
}

// ActionResult is the broker's answer to a lifecycle request. It names the
// BOSH task the director queued.
type ActionResult struct {
	InstanceID string `json:"instance_id"`
	Deployment string `json:"deployment"`
	Operation  string `json:"operation"`
	TaskID     int    `json:"task_id"`
	State      string `json:"state"`
}

// Redeploy recreates the VMs of an instance with POST /b/{id}/recreate, or
// restarts their jobs with POST /b/{id}/restart when restart is true.
func (c Client) Redeploy(id string, restart bool) (ActionResult, error) {
	action := "recreate"
	if restart {
		action = "restart"
	}

	var out ActionResult
	_, err := c.long().request("POST", instancePath(id, "/"+action), nil, &out)

	// A broker without the instance routes refused before it did
	// anything, so the same work can go through the deployment routes.
	var old BrokerTooOldError
	if errors.As(err, &old) {
		return c.redeployThroughTheDeployment(id, action)
	}
	if err != nil {
		return out, fmt.Errorf("failed to %s %s: %w", action, id, err)
	}
	return out, nil
}

// redeployThroughTheDeployment does a redeploy with the deployment routes
// that older brokers have, using the deployment name from /b/status.
func (c Client) redeployThroughTheDeployment(id, action string) (ActionResult, error) {
	ref, err := c.Lookup(id)
	if err != nil {
		return ActionResult{}, err
	}
	if ref.DeploymentName == "" {
		return ActionResult{}, fmt.Errorf("the broker lists no deployment for instance %s", id)
	}

	res, err := c.DeploymentOp(ref.DeploymentName, action)
	if err != nil {
		return ActionResult{}, err
	}
	return ActionResult{InstanceID: id, Deployment: res.Deployment, Operation: res.Operation, TaskID: res.TaskID, State: res.State}, nil
}

// OperationStatus is the answer to an OSB last_operation poll.
type OperationStatus struct {
	State       string `json:"state"`
	Description string `json:"description"`
}

// operationOf reads the operation string from the body of an OSB 202
// answer. A 202 means the broker accepted the work, so a body that is not
// JSON, or an operation that is not a string, is read as no operation
// and never as a failure.
func operationOf(body []byte) string {
	var out struct {
		Operation string `json:"operation"`
	}
	if json.Unmarshal(body, &out) != nil {
		return ""
	}
	return out.Operation
}

// OperationRef carries what the OSB specification asks a platform to send
// back when it polls last_operation. An empty field is left out.
type OperationRef struct {
	Operation string
	ServiceID string
	PlanID    string
}

// LastOperation reads the state of the latest operation on an instance
// from the OSB last_operation route. A 410 means the instance is gone.
func (c Client) LastOperation(id string, ref OperationRef) (OperationStatus, error) {
	q := url.Values{}
	for key, value := range map[string]string{"operation": ref.Operation, "plan_id": ref.PlanID, "service_id": ref.ServiceID} {
		if value != "" {
			q.Set(key, value)
		}
	}
	path := "/v2/service_instances/" + url.PathEscape(id) + "/last_operation"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}

	var out OperationStatus
	_, err := c.request("GET", path, nil, &out)

	var api APIError
	if errors.As(err, &api) && api.Status == http.StatusGone {
		return out, fmt.Errorf("failed to read the operation of %s: %w", id, ErrInstanceGone)
	}
	if err != nil {
		return out, fmt.Errorf("failed to read the operation of %s: %w", id, err)
	}
	return out, nil
}
