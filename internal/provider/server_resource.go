
package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
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
	PlanID       types.Int64  `tfsdk:"plan_id"`
	LocationID   types.Int64  `tfsdk:"location_id"`
	Template     types.String `tfsdk:"template"`
	Hostname     types.String `tfsdk:"hostname"`
	Status       types.String `tfsdk:"status"`
	RootPassword types.String `tfsdk:"root_password"`
	IPv4         types.String `tfsdk:"ipv4"`
	IPv6         types.String `tfsdk:"ipv6"`
	CreatedAt    types.String `tfsdk:"created_at"`
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
			"template": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "OS template slug (see `cloudblast_templates` data source).",
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

	params := CreateServerParams{
		PlanID:       int(data.PlanID.ValueInt64()),
		LocationID:   int(data.LocationID.ValueInt64()),
		TemplateSlug: data.Template.ValueString(),
	}
	if !data.Hostname.IsNull() && !data.Hostname.IsUnknown() {
		params.Hostname = data.Hostname.ValueString()
	}

	server, err := r.client.CreateServer(ctx, params)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create server", err.Error())
		return
	}

	data.ID = types.StringValue(server.ID)
	data.CreatedAt = types.StringValue(server.CreatedAt)

	// Poll until installation completes or fails
	finalStatus, err := r.pollServerStatus(ctx, server.ID)
	if err != nil {
		resp.Diagnostics.AddError("Server creation failed", err.Error())
		return
	}
	data.Status = types.StringValue(finalStatus)

	// Get full server details (IPs)
	fullServer, err := r.client.GetServer(ctx, server.ID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to get server details", err.Error())
		return
	}
	data.IPv4 = types.StringValue(fullServer.IPv4)
	data.IPv6 = types.StringValue(fullServer.IPv6)
	if fullServer.Hostname != "" {
		data.Hostname = types.StringValue(fullServer.Hostname)
	}

	// Get root password
	creds, err := r.client.GetServerCredentials(ctx, server.ID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to get server credentials", err.Error())
		return
	}
	if creds.RootPassword != nil {
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
		resp.Diagnostics.AddError("Failed to read server", err.Error())
		return
	}

	data.Status = types.StringValue(server.Status)
	data.IPv4 = types.StringValue(server.IPv4)
	data.IPv6 = types.StringValue(server.IPv6)
	data.Hostname = types.StringValue(server.Hostname)
	data.CreatedAt = types.StringValue(server.CreatedAt)

	// Re-fetch root password (may have been regenerated on reinstall)
	creds, err := r.client.GetServerCredentials(ctx, data.ID.ValueString())
	if err == nil && creds.RootPassword != nil {
		data.RootPassword = types.StringValue(*creds.RootPassword)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ServerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Server resource is currently create-only for plan/location/template.
	// hostname can be updated via rename.
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
			resp.Diagnostics.AddError("Failed to rename server", err.Error())
			return
		}
		data.Hostname = types.StringValue(data.Hostname.ValueString())
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
		resp.Diagnostics.AddError("Failed to delete server", err.Error())
	}
}

func (r *ServerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id := req.ID
	server, err := r.client.GetServer(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Failed to import server", err.Error())
		return
	}

	data := ServerResourceModel{
		ID:         types.StringValue(server.ID),
		PlanID:     types.Int64Value(int64(server.PlanID)),
		LocationID: types.Int64Value(int64(server.LocationID)),
		Template:   types.StringValue(server.TemplateSlug),
		Hostname:   types.StringValue(server.Hostname),
		Status:     types.StringValue(server.Status),
		IPv4:       types.StringValue(server.IPv4),
		IPv6:       types.StringValue(server.IPv6),
		CreatedAt:  types.StringValue(server.CreatedAt),
	}

	creds, err := r.client.GetServerCredentials(ctx, id)
	if err == nil && creds.RootPassword != nil {
		data.RootPassword = types.StringValue(*creds.RootPassword)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// pollServerStatus waits for a server to reach a terminal state.
func (r *ServerResource) pollServerStatus(ctx context.Context, serverID string) (string, error) {
	deadline := time.Now().Add(createPollTimeout)
	for {
		if time.Now().After(deadline) {
			return "", fmt.Errorf("timeout waiting for server %s to become ready", serverID)
		}

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(createPollInterval):
		}

		status, err := r.client.GetServerStatus(ctx, serverID)
		if err != nil {
			return "", fmt.Errorf("failed to poll server status: %w", err)
		}

		appStatus := status.ApplicationStatus
		switch appStatus {
		case "active":
			return appStatus, nil
		case "install_failed", "deleted", "deletion_failed":
			return appStatus, fmt.Errorf("server %s reached terminal state: %s", serverID, appStatus)
		default:
			// installing, restoring_backup, etc. — keep polling
		}
	}
}

// renameServer wraps the PATCH /servers/{id}/rename endpoint.
func (c *Client) renameServer(ctx context.Context, id, name string) error {
	_, err := c.request(ctx, "PATCH", "/servers/"+id+"/rename", map[string]string{"name": name}, nil)
	return err
}
