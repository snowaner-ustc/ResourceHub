package reporter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/snowaner-ustc/ResourceHub/agent/internal/models"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
	slowClient *http.Client
	token      string
}

func New(baseURL string) *Client {
	return &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		slowClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) Register(name, hostname, version, group string) (models.RegisterResponse, error) {
	body, _ := json.Marshal(map[string]string{
		"name":          name,
		"hostname":      hostname,
		"agent_version": version,
		"group":         group,
	})
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/api/v1/agent/register", bytes.NewReader(body))
	if err != nil {
		return models.RegisterResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return models.RegisterResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return models.RegisterResponse{}, fmt.Errorf("register failed: %s", resp.Status)
	}
	var out models.RegisterResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return models.RegisterResponse{}, err
	}
	c.token = out.Token
	return out, nil
}

func (c *Client) SetToken(token string) {
	c.token = token
}

func (c *Client) Token() string {
	return c.token
}

func (c *Client) Report(snap models.Snapshot) error {
	if c.token == "" {
		return fmt.Errorf("agent not registered")
	}
	body, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/api/v1/agent/metrics", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("report failed: %s", resp.Status)
	}
	return nil
}

func (c *Client) ClaimJob() (*models.Job, error) {
	if c.token == "" {
		return nil, fmt.Errorf("agent not registered")
	}
	req, err := http.NewRequest(http.MethodGet, c.baseURL+"/api/v1/agent/jobs", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("claim job failed: %s", resp.Status)
	}
	var job models.Job
	if err := json.NewDecoder(resp.Body).Decode(&job); err != nil {
		return nil, err
	}
	if job.ID == "" {
		return nil, nil
	}
	return &job, nil
}

func (c *Client) SubmitJobResult(id string, result models.JobResult) error {
	if c.token == "" {
		return fmt.Errorf("agent not registered")
	}
	body, err := json.Marshal(result)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/api/v1/agent/jobs/"+id+"/result", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.slowClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("submit job failed: %s", resp.Status)
	}
	return nil
}

func LoadToken(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(bytes.TrimSpace(data)), nil
}

func SaveToken(path, token string) error {
	return os.WriteFile(path, []byte(token), 0o600)
}
