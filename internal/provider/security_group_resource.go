package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &SecurityGroupResource{}

type SecurityGroupResource struct {
	client *Client
}

type SecurityGroupResourceModel struct {
	ID          types.Int64  `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
}

func NewSecurityGroupResource() resource.Resource {
	return &SecurityGroupResource{}
}

func (r *SecurityGroupResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_security_group"
}

func (r *SecurityGroupResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a CloudBlast security group (firewall).",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "Security group ID.",
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Security group name.",
			},
			"description": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Security group description.",
			},
		},
	}
}

func (r *SecurityGroupResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *SecurityGroupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data SecurityGroupResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	desc := ""
	if !data.Description.IsNull() && !data.Description.IsUnknown() {
		desc = data.Description.ValueString()
	}

	sg, err := r.client.CreateSecurityGroup(ctx, data.Name.ValueString(), desc)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create security group", err.Error())
		return
	}

	data.ID = types.Int64Value(int64(sg.ID))
	data.Name = types.StringValue(sg.Name)
	data.Description = types.StringValue(sg.Description)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *SecurityGroupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data SecurityGroupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	sg, err := r.client.GetSecurityGroup(ctx, int(data.ID.ValueInt64()))
	if err != nil {
		resp.Diagnostics.AddError("Failed to read security group", err.Error())
		return
	}

	data.Name = types.StringValue(sg.Name)
	data.Description = types.StringValue(sg.Description)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *SecurityGroupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// CloudBlast security groups don't support PATCH — force recreation
	var data SecurityGroupResourceModel
	resp.Diagnostics.Append(req.State.Set(ctx, &data)...)
}

func (r *SecurityGroupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data SecurityGroupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteSecurityGroup(ctx, int(data.ID.ValueInt64()))
	if err != nil {
		resp.Diagnostics.AddError("Failed to delete security group", err.Error())
	}
}
