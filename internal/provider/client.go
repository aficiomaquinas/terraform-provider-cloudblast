// Copyright 2026 aficiomaquinas
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultBaseURL = "https://console.cloudblast.io/api/v2"
	defaultTimeout = 30 * time.Second
	userAgent      = "terraform-provider-cloudblast"
)

// Client wraps the CloudBlast REST API v2.
type Client struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

// NewClient creates a new CloudBlast API client.
func NewClient(baseURL, token string) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Client{
		BaseURL: baseURL,
		Token:   token,
		HTTPClient: &http.Client{
			Timeout: defaultTimeout,
		},
	}
}

// APIError represents a CloudBlast API error response.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("CloudBlast API error %d (%s): %s", e.Status, e.Code, e.Message)
}

// apiResponse is the standard wrapper for CloudBlast API responses.
type apiResponse struct {
	Data  json.RawMessage `json:"data,omitempty"`
	Error *apiErrorBody   `json:"error,omitempty"`
}

type apiErrorBody struct {
	Code    string              `json:"code"`
	Message string              `json:"message"`
	Details map[string][]string `json:"details,omitempty"`
}

// apiErrorResponse handles non-standard error formats (e.g., {"message": "..."}).
type apiErrorResponse struct {
	Message string `json:"message"`
}

// request makes an HTTP request to the CloudBlast API.
// Handles all HTTP status codes and error formats gracefully.
func (c *Client) request(ctx context.Context, method, path string, body any, query map[string]string) (json.RawMessage, error) {
	u, err := url.Parse(c.BaseURL + path)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	if query != nil {
		q := u.Query()
		for k, v := range query {
			if v != "" {
				q.Set(k, v)
			}
		}
		u.RawQuery = q.Encode()
	}

	var bodyReader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// 204 No Content (e.g., delete)
	if resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Try to parse as standard API response
	var apiResp apiResponse
	_ = json.Unmarshal(respBody, &apiResp)

	// Handle error responses (any non-2xx status)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, parseAPIError(resp.StatusCode, respBody, &apiResp)
	}

	// Handle successful responses
	if apiResp.Data != nil {
		return apiResp.Data, nil
	}

	// 2xx but no data field — try raw body
	if len(respBody) > 0 {
		return respBody, nil
	}

	// 2xx with empty body
	return nil, nil
}

// parseAPIError extracts error information from API responses.
// Handles multiple error formats:
//   - Standard: {"error": {"code": "...", "message": "..."}}
//   - Non-standard: {"message": "..."}
//   - Raw body fallback
func parseAPIError(statusCode int, body []byte, apiResp *apiResponse) error {
	// Standard format: {"error": {"code": "...", "message": "..."}}
	if apiResp.Error != nil {
		return &APIError{
			Status:  statusCode,
			Code:    apiResp.Error.Code,
			Message: apiResp.Error.Message,
		}
	}

	// Non-standard format: {"message": "..."}
	var errResp apiErrorResponse
	if json.Unmarshal(body, &errResp) == nil && errResp.Message != "" {
		return &APIError{
			Status:  statusCode,
			Code:    http.StatusText(statusCode),
			Message: errResp.Message,
		}
	}

	// Raw body fallback
	bodyStr := strings.TrimSpace(string(body))
	if bodyStr != "" {
		return &APIError{
			Status:  statusCode,
			Code:    http.StatusText(statusCode),
			Message: bodyStr,
		}
	}

	// No body at all
	return &APIError{
		Status:  statusCode,
		Code:    http.StatusText(statusCode),
		Message: fmt.Sprintf("HTTP %d with empty response body", statusCode),
	}
}

// --- Server endpoints ---

type CreateServerParams struct {
	PlanID       int    `json:"plan_id"`
	LocationID   int    `json:"location_id"`
	TemplateSlug string `json:"template_slug"`
	Hostname     string `json:"hostname,omitempty"`
	SSHKeyIDs    []int  `json:"ssh_key_ids,omitempty"`
}

type ServerData struct {
	ID          int            `json:"id"`
	UUID        string         `json:"uuid"`
	UUIDShort   string         `json:"uuid_short"`
	Name        string         `json:"name"`
	Hostname    string         `json:"hostname"`
	Status      *string        `json:"status"`
	CPU         int            `json:"cpu"`
	Memory      int64          `json:"memory"`
	Disk        int64          `json:"disk"`
	NodeID      int            `json:"node_id"`
	CreatedAt   string         `json:"created_at"`
	IPAddresses []ServerIPData `json:"ip_addresses"`
	OS          string         `json:"os"`
}

type ServerIPData struct {
	Address string  `json:"address"`
	Type    string  `json:"type"`
	Gateway string  `json:"gateway"`
	CIDR    string  `json:"cidr"`
	RDNS    *string `json:"rdns"`
}

type ServerCredentials struct {
	PasswordStatus string  `json:"password_status"`
	RootPassword   *string `json:"root_password"`
}

type ServerStatus struct {
	State        string  `json:"state"`
	PowerTask    *string `json:"power_task"`
	ServerStatus *string `json:"server_status"`
}

func (c *Client) CreateServer(ctx context.Context, params CreateServerParams) (*ServerData, error) {
	data, err := c.request(ctx, http.MethodPost, "/servers", params, nil)
	if err != nil {
		return nil, err
	}
	var server ServerData
	if err := json.Unmarshal(data, &server); err != nil {
		return nil, fmt.Errorf("failed to parse server: %w", err)
	}
	return &server, nil
}

func (c *Client) GetServer(ctx context.Context, id string) (*ServerData, error) {
	data, err := c.request(ctx, http.MethodGet, "/servers/"+id, nil, nil)
	if err != nil {
		return nil, err
	}
	var server ServerData
	if err := json.Unmarshal(data, &server); err != nil {
		return nil, fmt.Errorf("failed to parse server: %w", err)
	}
	return &server, nil
}

func (c *Client) DeleteServer(ctx context.Context, id string) error {
	_, err := c.request(ctx, http.MethodDelete, "/servers/"+id, nil, nil)
	return err
}

func (c *Client) GetServerCredentials(ctx context.Context, id string) (*ServerCredentials, error) {
	data, err := c.request(ctx, http.MethodGet, "/servers/"+id+"/credentials", nil, nil)
	if err != nil {
		return nil, err
	}
	var creds ServerCredentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("failed to parse credentials: %w", err)
	}
	return &creds, nil
}

func (c *Client) GetServerStatus(ctx context.Context, id string) (*ServerStatus, error) {
	data, err := c.request(ctx, http.MethodGet, "/servers/"+id+"/status", nil, nil)
	if err != nil {
		return nil, err
	}
	var status ServerStatus
	if err := json.Unmarshal(data, &status); err != nil {
		return nil, fmt.Errorf("failed to parse status: %w", err)
	}
	return &status, nil
}

// --- SSH Key endpoints ---

type SSHKeyData struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	PublicKey string `json:"public_key"`
	CreatedAt string `json:"created_at"`
}

func (c *Client) CreateSSHKey(ctx context.Context, name, publicKey string) (*SSHKeyData, error) {
	body := map[string]string{"name": name, "public_key": publicKey}
	data, err := c.request(ctx, http.MethodPost, "/ssh-keys", body, nil)
	if err != nil {
		return nil, err
	}
	var key SSHKeyData
	if err := json.Unmarshal(data, &key); err != nil {
		return nil, fmt.Errorf("failed to parse SSH key: %w", err)
	}
	return &key, nil
}

func (c *Client) GetSSHKey(ctx context.Context, id int) (*SSHKeyData, error) {
	data, err := c.request(ctx, http.MethodGet, fmt.Sprintf("/ssh-keys/%d", id), nil, nil)
	if err != nil {
		return nil, err
	}
	var key SSHKeyData
	if err := json.Unmarshal(data, &key); err != nil {
		return nil, fmt.Errorf("failed to parse SSH key: %w", err)
	}
	return &key, nil
}

func (c *Client) ListSSHKeys(ctx context.Context) ([]SSHKeyData, error) {
	data, err := c.request(ctx, http.MethodGet, "/ssh-keys", nil, nil)
	if err != nil {
		return nil, err
	}
	var keys []SSHKeyData
	if err := json.Unmarshal(data, &keys); err != nil {
		return nil, fmt.Errorf("failed to parse SSH keys: %w", err)
	}
	return keys, nil
}

func (c *Client) DeleteSSHKey(ctx context.Context, id int) error {
	_, err := c.request(ctx, http.MethodDelete, fmt.Sprintf("/ssh-keys/%d", id), nil, nil)
	return err
}

// --- Security Group endpoints ---

type SecurityGroupData struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CreatedAt   string `json:"created_at"`
}

func (c *Client) CreateSecurityGroup(ctx context.Context, name, description string) (*SecurityGroupData, error) {
	body := map[string]string{"name": name}
	if description != "" {
		body["description"] = description
	}
	data, err := c.request(ctx, http.MethodPost, "/security-groups", body, nil)
	if err != nil {
		return nil, err
	}
	var sg SecurityGroupData
	if err := json.Unmarshal(data, &sg); err != nil {
		return nil, fmt.Errorf("failed to parse security group: %w", err)
	}
	return &sg, nil
}

func (c *Client) GetSecurityGroup(ctx context.Context, id int) (*SecurityGroupData, error) {
	data, err := c.request(ctx, http.MethodGet, fmt.Sprintf("/security-groups/%d", id), nil, nil)
	if err != nil {
		return nil, err
	}
	var sg SecurityGroupData
	if err := json.Unmarshal(data, &sg); err != nil {
		return nil, fmt.Errorf("failed to parse security group: %w", err)
	}
	return &sg, nil
}

func (c *Client) DeleteSecurityGroup(ctx context.Context, id int) error {
	_, err := c.request(ctx, http.MethodDelete, fmt.Sprintf("/security-groups/%d", id), nil, nil)
	return err
}

// --- Firewall Rule endpoints ---

type FirewallRuleData struct {
	ID              int    `json:"id"`
	Type            string `json:"type"`
	Action          string `json:"action"`
	Protocol        string `json:"protocol"`
	Source          string `json:"source,omitempty"`
	Destination     string `json:"destination,omitempty"`
	SourcePort      string `json:"source_port,omitempty"`
	DestinationPort string `json:"destination_port,omitempty"`
	Comment         string `json:"comment,omitempty"`
	Priority        int    `json:"priority,omitempty"`
}

type CreateFirewallRuleParams struct {
	Type            string `json:"type"`
	Action          string `json:"action"`
	Protocol        string `json:"protocol"`
	Source          string `json:"source,omitempty"`
	Destination     string `json:"destination,omitempty"`
	SourcePort      string `json:"source_port,omitempty"`
	DestinationPort string `json:"destination_port,omitempty"`
	Comment         string `json:"comment,omitempty"`
	Priority        int    `json:"priority,omitempty"`
}

func (c *Client) CreateFirewallRule(ctx context.Context, groupID int, params CreateFirewallRuleParams) (*FirewallRuleData, error) {
	data, err := c.request(ctx, http.MethodPost, fmt.Sprintf("/security-groups/%d/rules", groupID), params, nil)
	if err != nil {
		return nil, err
	}
	var rule FirewallRuleData
	if err := json.Unmarshal(data, &rule); err != nil {
		return nil, fmt.Errorf("failed to parse firewall rule: %w", err)
	}
	return &rule, nil
}

func (c *Client) DeleteFirewallRule(ctx context.Context, groupID, ruleID int) error {
	_, err := c.request(ctx, http.MethodDelete, fmt.Sprintf("/security-groups/%d/rules/%d", groupID, ruleID), nil, nil)
	return err
}

// --- Attach/Detach Server to Security Group ---

func (c *Client) AttachServerToGroup(ctx context.Context, groupID int, serverUUID string) error {
	body := map[string]string{"server_uuid": serverUUID}
	_, err := c.request(ctx, http.MethodPost, fmt.Sprintf("/security-groups/%d/servers/attach", groupID), body, nil)
	return err
}

func (c *Client) DetachServerFromGroup(ctx context.Context, groupID int, serverUUID string) error {
	body := map[string]string{"server_uuid": serverUUID}
	_, err := c.request(ctx, http.MethodDelete, fmt.Sprintf("/security-groups/%d/servers/detach", groupID), body, nil)
	return err
}

// --- Data Source endpoints ---

type PlanData struct {
	ID             int     `json:"id"`
	Name           string  `json:"name"`
	VCpus          int     `json:"vcpus"`
	RAM            int64   `json:"ram"`
	Disk           int64   `json:"disk"`
	BandwidthLimit int64   `json:"bandwidth_limit"`
	MonthlyPrice   float64 `json:"monthly_price"`
	HourlyPrice    float64 `json:"hourly_price"`
	BackupPrice    string  `json:"backup_price"`
	BackupLimit    int     `json:"backup_limit"`
	LocationID     *int    `json:"location_id"`
}

func (c *Client) ListPlans(ctx context.Context, page int) ([]PlanData, error) {
	query := map[string]string{}
	if page > 0 {
		query["page"] = strconv.Itoa(page)
	}
	data, err := c.request(ctx, http.MethodGet, "/plans", nil, query)
	if err != nil {
		return nil, err
	}
	var plans []PlanData
	if err := json.Unmarshal(data, &plans); err != nil {
		return nil, fmt.Errorf("failed to parse plans: %w", err)
	}
	return plans, nil
}

type LocationData struct {
	ID          int    `json:"id"`
	ShortCode   string `json:"short_code"`
	Description string `json:"description"`
	OutOfStock  bool   `json:"out_of_stock"`
}

func (c *Client) ListLocations(ctx context.Context) ([]LocationData, error) {
	data, err := c.request(ctx, http.MethodGet, "/locations", nil, nil)
	if err != nil {
		return nil, err
	}
	var locations []LocationData
	if err := json.Unmarshal(data, &locations); err != nil {
		return nil, fmt.Errorf("failed to parse locations: %w", err)
	}
	return locations, nil
}

type TemplateData struct {
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	GroupName string `json:"group_name"`
}

func (c *Client) ListTemplates(ctx context.Context, locationID int) ([]TemplateData, error) {
	data, err := c.request(ctx, http.MethodGet, fmt.Sprintf("/locations/%d/templates", locationID), nil, nil)
	if err != nil {
		return nil, err
	}
	var templates []TemplateData
	if err := json.Unmarshal(data, &templates); err != nil {
		return nil, fmt.Errorf("failed to parse templates: %w", err)
	}
	return templates, nil
}

type AccountData struct {
	Name          string         `json:"name"`
	Email         string         `json:"email"`
	CreditBalance string         `json:"credit_balance"`
	Limits        map[string]int `json:"limits"`
}

func (c *Client) GetAccount(ctx context.Context) (*AccountData, error) {
	data, err := c.request(ctx, http.MethodGet, "/account", nil, nil)
	if err != nil {
		return nil, err
	}
	var account AccountData
	if err := json.Unmarshal(data, &account); err != nil {
		return nil, fmt.Errorf("failed to parse account: %w", err)
	}
	return &account, nil
}
