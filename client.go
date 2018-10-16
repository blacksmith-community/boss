package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"os"
	"strings"
)

type Client struct {
	URL      string
	Username string
	Password string
	Debug    bool
	Trace    bool

	ua *http.Client
}

type Plan struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description/"`
}

type Service struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Description    string   `json:"description/"`
	Bindable       bool     `json:"bindable"`
	Tags           []string `json:"tags"`
	PlanUpdateable bool     `json:"plan_updateable"`
	Plans          []Plan   `json:"plans"`
}

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

type Instance struct {
	ID      string   `json:"id"`
	Service *Service `json:"service"`
	Plan    *Plan    `json:"plan"`
}

func (c Client) do(method, path string, in interface{}) (*http.Response, error) {
	if c.ua == nil {
		c.ua = &http.Client{}
		c.URL = strings.TrimSuffix(c.URL, "/")
	}

	var body io.Reader = nil
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return nil, err
		}

		body = bytes.NewBuffer(b)
	}

	req, err := http.NewRequest(method, c.URL+path, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-Broker-API-Version", "2.14")
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
		return nil, err
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
		return 0, err
	}

	defer res.Body.Close()
	if out != nil {
		b, err := ioutil.ReadAll(res.Body)
		if err != nil {
			return 0, err
		}

		err = json.Unmarshal(b, &out)
		if err != nil {
			return 0, err
		}
	}

	if res.StatusCode < 200 || res.StatusCode > 299 {
		return res.StatusCode, fmt.Errorf("API %s", res.Status)
	}

	return res.StatusCode, nil
}

func (c Client) text(path string, args ...interface{}) (string, error) {
	res, err := c.do("GET", fmt.Sprintf(path, args...), nil)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		return "", fmt.Errorf("API %s", res.Status)
	}
	b, err := ioutil.ReadAll(res.Body)
	return string(b), err
}

func (c Client) Catalog() (Catalog, error) {
	var out Catalog
	_, err := c.request("GET", "/v2/catalog", nil, &out)
	return out, err
}

func (c Client) Plan(service, plan string) (*Service, *Plan, error) {
	cat, err := c.Catalog()
	if err != nil {
		return nil, nil, err
	}

	return cat.Plan(service, plan)
}

func (c Client) Resolve(want string) (string, error) {
	var out struct {
		Instances map[string]struct{} `json:"instances"`
	}
	_, err := c.request("GET", "/b/status", nil, &out)
	if err != nil {
		return "", err
	}

	for id := range out.Instances {
		if id == want {
			return id, nil
		}
	}
	for id := range out.Instances {
		if strings.HasPrefix(id, want) {
			return id, nil
		}
	}

	return "", fmt.Errorf("No instance found matching `%s'", want)
}

func (c Client) Log() (string, error) {
	var out struct {
		Log string `json:"log"`
	}
	_, err := c.request("GET", "/b/status", nil, &out)
	return out.Log, err
}

func (c Client) Instances() ([]Instance, error) {
	cat, err := c.Catalog()
	if err != nil {
		return nil, err
	}

	var out struct {
		Instances map[string]struct {
			PlanID    string `json:"plan_id"`
			ServiceID string `json:"service_id"`
		} `json:"instances"`
	}
	_, err = c.request("GET", "/b/status", nil, &out)
	if err != nil {
		return nil, err
	}

	instances := make([]Instance, 0)
	for id, stuff := range out.Instances {
		service, plan, _ := cat.Plan(stuff.ServiceID, stuff.PlanID)
		if service != nil && plan != nil {
			instances = append(instances, Instance{
				ID:      id,
				Service: service,
				Plan:    plan,
			})
		} else {
			instances = append(instances, Instance{ID: id})
		}
	}

	return instances, nil
}

func (c Client) Create(id, service, plan string) (Instance, error) {
	in := struct {
		ServiceID string `json:"service_id"`
		PlanID    string `json:"plan_id"`
		OrgID     string `json:"organization_guid"`
		SpaceID   string `json:"space_guid"`
	}{
		ServiceID: service,
		PlanID:    plan,
		OrgID:     "boss",
		SpaceID:   "boss",
	}

	_, err := c.request("PUT", "/v2/service_instances/"+id, in, nil)
	return Instance{ID: id}, err
}

func (c Client) Delete(id string) error {
	_, err := c.request("DELETE", "/v2/service_instances/"+id, nil, nil)
	return err
}

func (c Client) Task(id string) (string, error) {
	return c.text("/b/%s/task.log", id)
}

func (c Client) Manifest(id string) (string, error) {
	return c.text("/b/%s/manifest.yml", id)
}

func (c Client) Creds(id string) (string, error) {
	return c.text("/b/%s/creds.yml", id)
}
