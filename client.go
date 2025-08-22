package main

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
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
}

func (e APIError) Error() string {
	if e.Description != "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Description)
	}
	return e.Code
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
	// Timeout sets the HTTP client timeout (default: 30s)
	Timeout time.Duration
	// MaxRetries sets the maximum number of retry attempts (default: 3)
	MaxRetries int
	// BrokerAPIVersion sets the X-Broker-API-Version header (default: 2.16)
	BrokerAPIVersion string

	// ua is the internal HTTP client
	ua *http.Client
}

// Plan represents a service plan in the Blacksmith catalog
type Plan struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Free        bool   `json:"free,omitempty"`
	Bindable    *bool  `json:"bindable,omitempty"`
}

// Service represents a service in the Blacksmith catalog
type Service struct {
	ID             string                 `json:"id"`
	Name           string                 `json:"name"`
	Description    string                 `json:"description"`
	Bindable       bool                   `json:"bindable"`
	Tags           []string               `json:"tags"`
	PlanUpdateable bool                   `json:"plan_updateable"`
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
	ID        string    `json:"id"`
	Service   *Service  `json:"service"`
	Plan      *Plan     `json:"plan"`
	State     string    `json:"state,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

func (c Client) do(method, path string, in interface{}) (*http.Response, error) {
	if c.ua == nil {
		timeout := c.Timeout
		if timeout == 0 {
			timeout = 30 * time.Second
		}
		c.ua = &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					// #nosec G402 -- InsecureSkipVerify is a configurable option for development environments
					// and connecting to services with self-signed certificates. User must explicitly enable it.
					InsecureSkipVerify: c.InsecureSkipVerify,
				},
				Proxy: http.ProxyFromEnvironment,
			},
		}
		c.URL = strings.TrimSuffix(c.URL, "/")
	}

	return c.doWithRetry(method, path, in)
}

// doWithRetry performs the request with retry logic
func (c Client) doWithRetry(method, path string, in interface{}) (*http.Response, error) {
	maxRetries := c.MaxRetries
	if maxRetries == 0 {
		maxRetries = 3
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff
			backoff := time.Duration(attempt*attempt) * time.Second
			if c.Debug {
				fmt.Fprintf(os.Stderr, "Retrying request after %s (attempt %d/%d)\n", backoff, attempt+1, maxRetries+1)
			}
			time.Sleep(backoff)
		}

		res, err := c.doSingle(method, path, in)
		if err == nil {
			return res, nil
		}

		lastErr = err

		// Don't retry certain errors
		if !c.shouldRetry(err) {
			break
		}
	}

	return nil, fmt.Errorf("request failed after %d attempts: %w", maxRetries+1, lastErr)
}

// shouldRetry determines if an error is retryable
func (c Client) shouldRetry(err error) bool {
	// Don't retry client errors (4xx)
	if apiErr, ok := err.(APIError); ok {
		return apiErr.Status >= 500 // Only retry server errors
	}

	// Retry network errors
	if strings.Contains(err.Error(), "connection refused") ||
		strings.Contains(err.Error(), "timeout") ||
		strings.Contains(err.Error(), "temporary failure") {
		return true
	}

	return false
}

// doSingle performs a single HTTP request
func (c Client) doSingle(method, path string, in interface{}) (*http.Response, error) {

	var body io.Reader = nil
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request: %w", err)
		}

		body = bytes.NewBuffer(b)
		if c.Debug {
			fmt.Fprintf(os.Stderr, "REQUEST: %s %s\n", method, c.URL+path)
			fmt.Fprintf(os.Stderr, "BODY: %s\n", string(b))
		}
	}

	// Build URL properly
	u, err := url.Parse(c.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL %s: %w", c.URL, err)
	}
	u.Path = path

	req, err := http.NewRequest(method, u.String(), body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set API version header for v2 endpoints
	if strings.HasPrefix(path, "/v2/") {
		version := c.BrokerAPIVersion
		if version == "" {
			version = "2.16"
		}
		req.Header.Set("X-Broker-API-Version", version)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.SetBasicAuth(c.Username, c.Password)

	if c.Trace {
		b, err := httputil.DumpRequestOut(req, true)
		if err == nil {
			fmt.Fprintf(os.Stderr, "=================================\n")
			fmt.Fprintf(os.Stderr, "%s\n\n", string(b))
		}
	}

	res, err := c.ua.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}

	if c.Trace {
		b, err := httputil.DumpResponse(res, true)
		if err == nil {
			fmt.Fprintf(os.Stderr, "=================================\n")
			fmt.Fprintf(os.Stderr, "%s\n\n", string(b))
		}
	}

	return res, nil
}

func (c Client) request(method, path string, in, out interface{}) (int, error) {
	res, err := c.do(method, path, in)
	if err != nil {
		return 0, fmt.Errorf("request failed: %w", err)
	}

	defer res.Body.Close()

	// Read response body
	b, err := io.ReadAll(res.Body)
	if err != nil {
		return res.StatusCode, fmt.Errorf("failed to read response: %w", err)
	}

	if c.Debug {
		fmt.Fprintf(os.Stderr, "RESPONSE: %d\n", res.StatusCode)
		fmt.Fprintf(os.Stderr, "BODY: %s\n", string(b))
	}

	// Parse response if output provided
	if out != nil && len(b) > 0 {
		if err := json.Unmarshal(b, out); err != nil {
			return res.StatusCode, fmt.Errorf("failed to parse response: %w", err)
		}
	}

	if method == "DELETE" && res.StatusCode == 410 {
		/* this is okay - already deleted */
		return res.StatusCode, nil
	}

	// Check for error response
	if res.StatusCode >= 400 {
		var apiErr APIError
		if json.Unmarshal(b, &apiErr) == nil && apiErr.Code != "" {
			apiErr.Status = res.StatusCode
			return res.StatusCode, apiErr
		}

		// Fallback for non-JSON error responses
		apiErr = APIError{
			Status:      res.StatusCode,
			Code:        "HTTPError",
			Description: fmt.Sprintf("HTTP %d: %s", res.StatusCode, res.Status),
		}
		if len(b) > 0 {
			apiErr.Description += " - " + string(b)
		}
		return res.StatusCode, apiErr
	}

	if res.StatusCode < 200 || res.StatusCode > 299 {
		return res.StatusCode, fmt.Errorf("unexpected status %d: %s", res.StatusCode, res.Status)
	}

	return res.StatusCode, nil
}

func (c Client) text(path string, args ...interface{}) (string, error) {
	res, err := c.do("GET", fmt.Sprintf(path, args...), nil)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		return "", fmt.Errorf("unexpected status %d: %s", res.StatusCode, res.Status)
	}
	b, err := io.ReadAll(res.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}
	return string(b), nil
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

func (c Client) Resolve(want string) (string, error) {
	var out struct {
		Instances map[string]struct{} `json:"instances"`
	}
	_, err := c.request("GET", "/b/status", nil, &out)
	if err != nil {
		return "", fmt.Errorf("failed to get status: %w", err)
	}

	// Exact match first
	for id := range out.Instances {
		if id == want {
			return id, nil
		}
	}

	// Prefix match second
	for id := range out.Instances {
		if strings.HasPrefix(id, want) {
			return id, nil
		}
	}

	return "", fmt.Errorf("no instance found matching '%s'", want)
}

func (c Client) Log() (string, error) {
	var out struct {
		Log string `json:"log"`
	}
	_, err := c.request("GET", "/b/status", nil, &out)
	if err != nil {
		return "", fmt.Errorf("failed to get log: %w", err)
	}
	return out.Log, nil
}

// Instances retrieves all service instances from the Blacksmith broker.
// Returns a slice of Instance structs sorted by creation time (newest first).
// Unknown services/plans will be logged as warnings in debug mode.
func (c Client) Instances() ([]Instance, error) {
	cat, err := c.Catalog()
	if err != nil {
		return nil, fmt.Errorf("failed to get catalog: %w", err)
	}

	var status struct {
		Instances map[string]struct {
			PlanID     string    `json:"plan_id"`
			ServiceID  string    `json:"service_id"`
			State      string    `json:"state,omitempty"`
			CreatedAt  time.Time `json:"created_at,omitempty"`
			UpdatedAt  time.Time `json:"updated_at,omitempty"`
			LastTaskID string    `json:"last_task_id,omitempty"`
		} `json:"instances"`
		Log string `json:"log"`
	}
	_, err = c.request("GET", "/b/status", nil, &status)
	if err != nil {
		return nil, fmt.Errorf("failed to get instance status: %w", err)
	}

	instances := make([]Instance, 0, len(status.Instances))
	for id, info := range status.Instances {
		service, plan, _ := cat.Plan(info.ServiceID, info.PlanID)

		instance := Instance{
			ID:        id,
			State:     info.State,
			CreatedAt: info.CreatedAt,
			UpdatedAt: info.UpdatedAt,
		}

		if service != nil && plan != nil {
			instance.Service = service
			instance.Plan = plan
		} else {
			// Log warning for unknown service/plan
			if c.Debug {
				fmt.Fprintf(os.Stderr, "WARNING: Unknown service/plan for instance %s: %s/%s\n",
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

// Create provisions a new service instance with the specified ID, service, plan, and parameters.
// This method supports asynchronous operations and will add the accepts_incomplete=true parameter.
// Returns an Instance struct with the ID, or an error if the operation fails.
func (c Client) Create(id, service, plan string, params map[string]interface{}) (Instance, error) {
	in := struct {
		ServiceID  string                 `json:"service_id"`
		PlanID     string                 `json:"plan_id"`
		OrgID      string                 `json:"organization_guid"`
		SpaceID    string                 `json:"space_guid"`
		Parameters map[string]interface{} `json:"parameters,omitempty"`
		Context    map[string]interface{} `json:"context,omitempty"`
	}{
		ServiceID:  service,
		PlanID:     plan,
		OrgID:      "boss",
		SpaceID:    "boss",
		Parameters: params,
	}

	_, err := c.request("PUT", "/v2/service_instances/"+id+"?accepts_incomplete=true", in, nil)
	if err != nil {
		return Instance{}, fmt.Errorf("failed to create instance %s: %w", id, err)
	}
	return Instance{ID: id}, nil
}

// Update modifies an existing service instance with new service, plan, or parameters.
// This method supports asynchronous operations and will add the accepts_incomplete=true parameter.
// Note: This method was fixed in v2.0 to properly use the service parameter instead of hardcoding "service".
func (c Client) Update(id, service, plan string, params map[string]interface{}) (Instance, error) {
	in := struct {
		ServiceID  string                 `json:"service_id"`
		PlanID     string                 `json:"plan_id,omitempty"`
		Parameters map[string]interface{} `json:"parameters,omitempty"`
	}{
		ServiceID:  service, // FIXED: was hardcoded as "service"
		PlanID:     plan,
		Parameters: params,
	}

	_, err := c.request("PATCH", "/v2/service_instances/"+id+"?accepts_incomplete=true", in, nil)
	if err != nil {
		return Instance{}, fmt.Errorf("failed to update instance %s: %w", id, err)
	}
	return Instance{ID: id}, nil
}

// Delete removes a service instance with the specified ID.
// This method supports asynchronous operations and will add the accepts_incomplete=true parameter.
// Returns an error if the deletion fails, or nil if successful.
func (c Client) Delete(id string) error {
	_, err := c.request("DELETE", "/v2/service_instances/"+id+"?accepts_incomplete=true", nil, nil)
	if err != nil {
		return fmt.Errorf("failed to delete instance %s: %w", id, err)
	}
	return nil
}

// Task retrieves the BOSH deployment task log for a service instance.
// Returns the complete task log as a string, or an error if the operation fails.
func (c Client) Task(id string) (string, error) {
	task, err := c.text("/b/%s/task.log", id)
	if err != nil {
		return "", fmt.Errorf("failed to get task log for %s: %w", id, err)
	}
	return task, nil
}

// StreamTask streams task logs, optionally following the log
func (c Client) StreamTask(id string, follow bool) error {
	path := fmt.Sprintf("/b/%s/task.log", id)
	if follow {
		path += "?follow=true"
	}

	u, err := url.Parse(c.URL)
	if err != nil {
		return fmt.Errorf("invalid URL %s: %w", c.URL, err)
	}
	u.Path = path

	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.SetBasicAuth(c.Username, c.Password)

	resp, err := c.ua.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	// Stream output
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		fmt.Println(scanner.Text())
	}

	return scanner.Err()
}

// Manifest retrieves the BOSH deployment manifest for a service instance.
// The returned YAML is automatically validated for syntax errors.
// Returns the manifest as a string, or an error if invalid or not found.
func (c Client) Manifest(id string) (string, error) {
	manifest, err := c.text("/b/%s/manifest.yml", id)
	if err != nil {
		return "", fmt.Errorf("failed to get manifest for %s: %w", id, err)
	}

	// Validate YAML
	if err := c.validateYAML(manifest); err != nil {
		return "", fmt.Errorf("invalid manifest for %s: %w", id, err)
	}

	return manifest, nil
}

// validateYAML checks if the content is valid YAML
func (c Client) validateYAML(content string) error {
	var data interface{}
	if err := yaml.Unmarshal([]byte(content), &data); err != nil {
		return fmt.Errorf("invalid YAML: %w", err)
	}
	return nil
}

// Creds retrieves the service credentials for a service instance as YAML.
// The returned YAML is automatically validated for syntax errors.
// Returns the credentials as a YAML string, or an error if invalid or not found.
func (c Client) Creds(id string) (string, error) {
	credsYAML, err := c.text("/b/%s/creds.yml", id)
	if err != nil {
		return "", fmt.Errorf("failed to get credentials for %s: %w", id, err)
	}

	// Validate YAML
	if err := c.validateYAML(credsYAML); err != nil {
		return "", fmt.Errorf("invalid credentials YAML for %s: %w", id, err)
	}

	return credsYAML, nil
}

// CredsMap returns credentials as a map for easier programmatic access
func (c Client) CredsMap(id string) (map[string]interface{}, error) {
	credsYAML, err := c.Creds(id)
	if err != nil {
		return nil, err
	}

	var creds map[string]interface{}
	if err := yaml.Unmarshal([]byte(credsYAML), &creds); err != nil {
		return nil, fmt.Errorf("failed to parse credentials: %w", err)
	}

	// Validate required fields (log warnings only)
	requiredFields := []string{"hostname", "port", "username", "password"}
	for _, field := range requiredFields {
		if _, ok := creds[field]; !ok {
			if c.Debug {
				fmt.Fprintf(os.Stderr, "WARNING: Missing credential field: %s\n", field)
			}
		}
	}

	return creds, nil
}

func (c Client) Redeploy(id string) (string, error) {
	result, err := c.text("/b/%s/redeploy", id)
	if err != nil {
		return "", fmt.Errorf("failed to redeploy %s: %w", id, err)
	}
	return result, nil
}

// waitForOperation polls for operation completion
func (c Client) waitForOperation(instanceID, operationID string, timeout time.Duration) error {
	if timeout == 0 {
		timeout = 30 * time.Minute
	}

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		// Check operation status
		var status struct {
			State       string `json:"state"`
			Description string `json:"description"`
		}

		path := fmt.Sprintf("/v2/service_instances/%s/last_operation", instanceID)
		if operationID != "" {
			path += "?operation=" + operationID
		}

		_, err := c.request("GET", path, nil, &status)
		if err != nil {
			return fmt.Errorf("failed to get operation status: %w", err)
		}

		switch status.State {
		case "succeeded":
			return nil
		case "failed":
			return fmt.Errorf("operation failed: %s", status.Description)
		case "in progress":
			if c.Debug {
				fmt.Fprintf(os.Stderr, "Operation in progress: %s\n", status.Description)
			}
		default:
			return fmt.Errorf("unknown operation state: %s", status.State)
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("operation timed out after %s", timeout)
		}
	}
	// This should never be reached but required for compilation
	return fmt.Errorf("unexpected end of operation polling")
}

// CreateAndWait creates an instance and waits for completion
func (c Client) CreateAndWait(id, service, plan string, params map[string]interface{}, timeout time.Duration) (Instance, error) {
	in := struct {
		ServiceID  string                 `json:"service_id"`
		PlanID     string                 `json:"plan_id"`
		OrgID      string                 `json:"organization_guid"`
		SpaceID    string                 `json:"space_guid"`
		Parameters map[string]interface{} `json:"parameters,omitempty"`
		Context    map[string]interface{} `json:"context,omitempty"`
	}{
		ServiceID:  service,
		PlanID:     plan,
		OrgID:      "boss",
		SpaceID:    "boss",
		Parameters: params,
	}

	var response struct {
		Operation string `json:"operation,omitempty"`
	}

	status, err := c.request("PUT",
		fmt.Sprintf("/v2/service_instances/%s?accepts_incomplete=true", id),
		in, &response)
	if err != nil {
		return Instance{}, fmt.Errorf("failed to create instance: %w", err)
	}

	// Handle async creation
	if status == 202 && response.Operation != "" {
		if c.Debug {
			fmt.Fprintf(os.Stderr, "Instance creation started, operation: %s\n", response.Operation)
		}

		// Wait for operation to complete
		if err := c.waitForOperation(id, response.Operation, timeout); err != nil {
			return Instance{}, fmt.Errorf("instance creation failed: %w", err)
		}
	}

	return Instance{ID: id}, nil
}
