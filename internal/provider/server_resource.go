// Copyright 2026 aficiomaquinas
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	createPollInterval = 5 * time.Second
	createPollTimeout  = 10 * time.Minute
)

var (
	_ resource.Resource                = &ServerResource{}
	_ resource.ResourceWithImportState = &ServerResource{}
)

type ServerResource struct {
	client *Client
}

type ServerResourceModel struct {
	ID           types.String `tfsdk:"id"`
	Hostname     types.String `tfsdk:"hostname"`
	Status       types.String `tfsdk:"status"`
	RootPassword types.String `tfsdk:"root_password"`
	IPv4         types.String `tfsdk:"ipv4"`
	IPv6         types.String `tfsdk:"ipv6"`
	OS           types.String `tfsdk:"os"`
	CreatedAt    types.String `tfsdk:"created_at"`
	PlanID       types.Int64  `tfsdk:"plan_id"`
	LocationID   types.Int64  `tfsdk:"location_id"`
	Template     types.String `tfsdk:"template"`
	SSHKeyIDs    types.String `tfsdk:"ssh_key_ids"`

	FailoverLocationIDs types.List  `tfsdk:"failover_location_ids"`
	EffectiveLocationID types.Int64 `tfsdk:"effective_location_id"`
}

func NewServerResource() resource.Resource {
	return &ServerResource{}
}

func (r *ServerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_server"
}

func (r *ServerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a CloudBlast VPS server.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Server UUID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"plan_id": schema.Int64Attribute{
				Required:            true,
				MarkdownDescription: "Server plan ID (see `cloudblast_plans` data source).",
			},
			"location_id": schema.Int64Attribute{
				Required:            true,
				MarkdownDescription: "Data center location ID (see `cloudblast_locations` data source).",
			},
			"failover_location_ids": schema.ListAttribute{
				Optional:    true,
				ElementType: types.Int64Type,
				MarkdownDescription: "Ordered list of fallback location IDs, tried in order when the primary `location_id` has " +
					"no available node for the selected plan (`NO_AVAILABLE_NODE`). Duplicates and the primary location are ignored. " +
					"Omit to disable failover (default). Other failures (e.g. plan out of stock) are never retried in another location.",
			},
			"effective_location_id": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "Location the server was actually created in. Equals `location_id` unless a failover location was used.",
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"template": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "OS template slug (see `cloudblast_templates` data source).",
			},
			"ssh_key_ids": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Comma-separated list of SSH key IDs to install during provisioning (e.g. `1,2,3`). Obtain IDs from `cloudblast_ssh_key` resources.",
			},
			"hostname": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Server hostname.",
			},
			"status": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Server status (installing, active, etc.).",
			},
			"root_password": schema.StringAttribute{
				Computed:            true,
				Sensitive:           true,
				MarkdownDescription: "Root password (available after installation completes).",
			},
			"ipv4": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Primary IPv4 address.",
			},
			"ipv6": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Primary IPv6 address.",
			},
			"os": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Operating system name.",
			},
			"created_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Server creation timestamp.",
			},
		},
	}
}

func (r *ServerResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", "Expected *Client")
		return
	}
	r.client = client
}

func (r *ServerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data ServerResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Build the ordered failover attempt list: primary first, then the
	// configured fallback locations (deduped, primary excluded). Empty when
	// failover_location_ids is omitted or contains only the primary.
	var fallbacks []int64
	if !data.FailoverLocationIDs.IsNull() && !data.FailoverLocationIDs.IsUnknown() {
		diags := data.FailoverLocationIDs.ElementsAs(ctx, &fallbacks, false)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
	}
	attempts := buildCreateAttempts(data.LocationID.ValueInt64(), fallbacks)

	// Create attempts: fail over ONLY on NO_AVAILABLE_NODE (the primary or a
	// fallback location has no node for this plan). Any other failure
	// (auth, validation, plan out of stock, 5xx, network) is terminal.
	var server *ServerData
	for i, locID := range attempts {
		params := CreateServerParams{
			PlanID:       int(data.PlanID.ValueInt64()),
			LocationID:   locID,
			TemplateSlug: data.Template.ValueString(),
		}
		if !data.Hostname.IsNull() && !data.Hostname.IsUnknown() {
			params.Hostname = data.Hostname.ValueString()
		}

		// Parse ssh_key_ids (comma-separated)
		if !data.SSHKeyIDs.IsNull() && !data.SSHKeyIDs.IsUnknown() && data.SSHKeyIDs.ValueString() != "" {
			for _, s := range strings.Split(data.SSHKeyIDs.ValueString(), ",") {
				s = strings.TrimSpace(s)
				if id, err := strconv.Atoi(s); err == nil {
					params.SSHKeyIDs = append(params.SSHKeyIDs, id)
				}
			}
		}

		s, err := r.client.CreateServer(ctx, params)
		if err == nil {
			server = s
			data.EffectiveLocationID = types.Int64Value(int64(locID))
			if i > 0 {
				resp.Diagnostics.AddWarning(
					"Server created in fallback location",
					fmt.Sprintf("Primary location %d had no available node; server was created in failover location %d.", attempts[0], locID),
				)
			}
			break
		}

		if shouldFailOver(err) {
			if i < len(attempts)-1 {
				resp.Diagnostics.AddWarning(
					"Location unavailable",
					fmt.Sprintf("CloudBlast location %d has no available node for plan %d; trying failover location %d.",
						locID, data.PlanID.ValueInt64(), attempts[i+1]),
				)
				continue
			}
			// Last attempt failed with NO_AVAILABLE_NODE — fall through to
			// the exhausted summary error below.
		}

		AddAPIErrorDiagnostics(&resp.Diagnostics, "Failed to create server", err)
		return
	}

	if server == nil {
		// Failover exhausted: every location returned NO_AVAILABLE_NODE.
		var b strings.Builder
		fmt.Fprintf(&b, "CloudBlast has no available node for plan %d in any of the %d attempted location(s):\n\n",
			data.PlanID.ValueInt64(), len(attempts))
		fmt.Fprintf(&b, "| Location | Result |\n|---|---|\n")
		for _, locID := range attempts {
			fmt.Fprintf(&b, "| %d | NO_AVAILABLE_NODE |\n", locID)
		}
		b.WriteString("\nAdd more failover_location_ids, choose a different plan, or try again later.")
		resp.Diagnostics.AddError("Failed to create server", b.String())
		return
	}

	data.ID = types.StringValue(server.UUID)
	data.CreatedAt = types.StringValue(server.CreatedAt)
	if server.Hostname != "" {
		data.Hostname = types.StringValue(server.Hostname)
	}

	// Poll until installation completes or fails
	status := ""
	if server.Status != nil {
		status = *server.Status
	}
	if status == "installing" || status == "" {
		finalStatus, pollErr := r.pollServerStatus(ctx, server.UUID)
		if pollErr != nil {
			AddAPIErrorDiagnostics(&resp.Diagnostics, "Server creation failed", pollErr)
			return
		}
		data.Status = types.StringValue(finalStatus)
	} else {
		data.Status = types.StringValue(status)
	}

	// Get full server details (IPs)
	fullServer, err := r.client.GetServer(ctx, server.UUID)
	if err != nil {
		AddAPIErrorDiagnostics(&resp.Diagnostics, "Failed to get server details", err)
		return
	}

	ipv4, ipv6 := extractIPs(fullServer.IPAddresses)
	data.IPv4 = types.StringValue(ipv4)
	data.IPv6 = types.StringValue(ipv6)
	data.OS = types.StringValue(fullServer.OS)

	// Get root password
	creds, err := r.client.GetServerCredentials(ctx, server.UUID)
	if err == nil && creds.RootPassword != nil {
		data.RootPassword = types.StringValue(*creds.RootPassword)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ServerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data ServerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	server, err := r.client.GetServer(ctx, data.ID.ValueString())
	if err != nil {
		AddAPIErrorDiagnostics(&resp.Diagnostics, "Failed to read server", err)
		return
	}

	if server.Status != nil {
		data.Status = types.StringValue(*server.Status)
	}
	data.Hostname = types.StringValue(server.Hostname)
	data.CreatedAt = types.StringValue(server.CreatedAt)
	data.OS = types.StringValue(server.OS)

	ipv4, ipv6 := extractIPs(server.IPAddresses)
	data.IPv4 = types.StringValue(ipv4)
	data.IPv6 = types.StringValue(ipv6)

	// Re-fetch root password
	creds, err := r.client.GetServerCredentials(ctx, data.ID.ValueString())
	if err == nil && creds.RootPassword != nil {
		data.RootPassword = types.StringValue(*creds.RootPassword)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ServerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data ServerResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var oldData ServerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &oldData)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// If hostname changed, rename
	if !data.Hostname.Equal(oldData.Hostname) && !data.Hostname.IsNull() {
		err := r.client.renameServer(ctx, data.ID.ValueString(), data.Hostname.ValueString())
		if err != nil {
			AddAPIErrorDiagnostics(&resp.Diagnostics, "Failed to rename server", err)
			return
		}
	}

	// effective_location_id is create-time only. The framework recomputes it
	// on update; preserve the recorded value unless the plan already carries
	// one (UseStateForUnknown normally handles this).
	if data.EffectiveLocationID.IsNull() || data.EffectiveLocationID.IsUnknown() {
		data.EffectiveLocationID = oldData.EffectiveLocationID
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ServerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data ServerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteServer(ctx, data.ID.ValueString())
	if err != nil {
		AddAPIErrorDiagnostics(&resp.Diagnostics, "Failed to delete server", err)
	}
}

func (r *ServerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id := req.ID
	server, err := r.client.GetServer(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Failed to import server", err.Error())
		return
	}

	ipv4, ipv6 := extractIPs(server.IPAddresses)

	data := ServerResourceModel{
		ID:        types.StringValue(server.UUID),
		Hostname:  types.StringValue(server.Hostname),
		IPv4:      types.StringValue(ipv4),
		IPv6:      types.StringValue(ipv6),
		OS:        types.StringValue(server.OS),
		CreatedAt: types.StringValue(server.CreatedAt),
	}
	// The API does not expose the creation location on the server object, so
	// effective_location_id cannot be derived on import; it stays null, like
	// the other config attributes in this resource's import.
	data.FailoverLocationIDs = types.ListNull(types.Int64Type)
	data.EffectiveLocationID = types.Int64Null()

	if server.Status != nil {
		data.Status = types.StringValue(*server.Status)
	}

	creds, err := r.client.GetServerCredentials(ctx, id)
	if err == nil && creds.RootPassword != nil {
		data.RootPassword = types.StringValue(*creds.RootPassword)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// buildCreateAttempts returns the ordered create attempt list: the primary
// location first, then the configured failover locations in order, with
// duplicates and the primary itself removed.
func buildCreateAttempts(primary int64, failoverIDs []int64) []int {
	attempts := []int{int(primary)}
	seen := map[int]bool{int(primary): true}
	for _, id := range failoverIDs {
		if !seen[int(id)] {
			seen[int(id)] = true
			attempts = append(attempts, int(id))
		}
	}
	return attempts
}

// shouldFailOver reports whether err is a NO_AVAILABLE_NODE capacity error —
// the only failure for which the create loop tries the next failover
// location. Everything else (plan out of stock, auth, validation, 5xx,
// network) is terminal.
func shouldFailOver(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Code == errCodeNoAvailableNode
}

// pollServerStatus waits for a server to reach a terminal state.
func (r *ServerResource) pollServerStatus(ctx context.Context, serverUUID string) (string, error) {
	deadline := time.Now().Add(createPollTimeout)
	for {
		if time.Now().After(deadline) {
			return "", fmt.Errorf("timeout waiting for server %s to become ready", serverUUID)
		}

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(createPollInterval):
		}

		status, err := r.client.GetServerStatus(ctx, serverUUID)
		if err != nil {
			return "", fmt.Errorf("failed to poll server status: %w", err)
		}

		appStatus := status.State
		if status.ServerStatus != nil && *status.ServerStatus != "" {
			appStatus = *status.ServerStatus
		}
		switch appStatus {
		case "running":
			return appStatus, nil
		case "install_failed", "deleted", "deletion_failed":
			return appStatus, fmt.Errorf("server %s reached terminal state: %s", serverUUID, appStatus)
		default:
			// installing, stopped, etc. — keep polling
		}
	}
}

// extractIPs pulls primary IPv4 and IPv6 from the ip_addresses array.
func extractIPs(ips []ServerIPData) (ipv4, ipv6 string) {
	for _, ip := range ips {
		if ip.Type == "ipv4" && ipv4 == "" {
			ipv4 = ip.Address
		}
		if ip.Type == "ipv6" && ipv6 == "" {
			ipv6 = ip.Address
		}
	}
	return
}

// renameServer wraps the PATCH /servers/{id}/rename endpoint.
func (c *Client) renameServer(ctx context.Context, uuid, name string) error {
	_, err := c.request(ctx, "PATCH", "/servers/"+uuid+"/rename", map[string]string{"name": name}, nil)
	return err
}
