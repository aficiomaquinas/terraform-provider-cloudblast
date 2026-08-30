package provider

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type SecurityGroupAttachmentResource struct {
	client *Client
}

type SecurityGroupAttachmentResourceModel struct {
	ID              types.String `tfsdk:"id"`
	SecurityGroupID types.Int64  `tfsdk:"security_group_id"`
	ServerID        types.String `tfsdk:"server_id"`
}

func NewSecurityGroupAttachmentResource() resource.Resource {
	return &SecurityGroupAttachmentResource{}
}

func (r *SecurityGroupAttachmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_security_group_attachment"
}

func (r *SecurityGroupAttachmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Attaches a CloudBlast security group to a server.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Resource ID (sgID-serverUUID).",
			},
			"security_group_id": schema.Int64Attribute{
				Required:            true,
				MarkdownDescription: "Security group ID.",
			},
			"server_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Server UUID.",
			},
		},
	}
}

func (r *SecurityGroupAttachmentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Provider Data", "Expected *Client")
		return
	}
	r.client = client
}

func (r *SecurityGroupAttachmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data SecurityGroupAttachmentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	groupID := int(data.SecurityGroupID.ValueInt64())
	serverUUID := data.ServerID.ValueString()

	if err := r.client.AttachServerToGroup(ctx, groupID, serverUUID); err != nil {
		resp.Diagnostics.AddError("Failed to attach security group", err.Error())
		return
	}

	data.ID = types.StringValue(fmt.Sprintf("%d-%s", groupID, serverUUID))
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *SecurityGroupAttachmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data SecurityGroupAttachmentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Verify the security group still exists
	groupID := int(data.SecurityGroupID.ValueInt64())
	sg, err := r.client.GetSecurityGroup(ctx, groupID)
	if err != nil || sg == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *SecurityGroupAttachmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Security group attachment is immutable — destroy and recreate
	var data SecurityGroupAttachmentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *SecurityGroupAttachmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data SecurityGroupAttachmentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	groupID := int(data.SecurityGroupID.ValueInt64())
	serverUUID := data.ServerID.ValueString()

	if err := r.client.DetachServerFromGroup(ctx, groupID, serverUUID); err != nil {
		resp.Diagnostics.AddError("Failed to detach security group", err.Error())
	}
}

func (r *SecurityGroupAttachmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.SplitN(req.ID, "-", 2)
	if len(parts) != 2 {
		resp.Diagnostics.AddError("Invalid import ID", "Format: security_group_id-server_uuid")
		return
	}

	groupID, err := strconv.Atoi(parts[0])
	if err != nil {
		resp.Diagnostics.AddError("Invalid security group ID", err.Error())
		return
	}

	data := SecurityGroupAttachmentResourceModel{
		ID:              types.StringValue(req.ID),
		SecurityGroupID: types.Int64Value(int64(groupID)),
		ServerID:        types.StringValue(parts[1]),
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
