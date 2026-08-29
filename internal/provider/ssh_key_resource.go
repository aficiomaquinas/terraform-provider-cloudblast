
package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &SSHKeyResource{}
	_ resource.ResourceWithImportState = &SSHKeyResource{}
)

type SSHKeyResource struct {
	client *Client
}

type SSHKeyResourceModel struct {
	ID        types.Int64  `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	PublicKey types.String `tfsdk:"public_key"`
	CreatedAt types.String `tfsdk:"created_at"`
}

func NewSSHKeyResource() resource.Resource {
	return &SSHKeyResource{}
}

func (r *SSHKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ssh_key"
}

func (r *SSHKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a CloudBlast SSH key.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "SSH key ID.",
				PlanModifiers: []planmodifier.Int64{
					// No UseStateForUnknown for int64 in framework — computed is fine
				},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "SSH key name.",
			},
			"public_key": schema.StringAttribute{
				Required:            true,
				Sensitive:           true,
				MarkdownDescription: "SSH public key content.",
			},
			"created_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Creation timestamp.",
			},
		},
	}
}

func (r *SSHKeyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *SSHKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data SSHKeyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	key, err := r.client.CreateSSHKey(ctx, data.Name.ValueString(), data.PublicKey.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to create SSH key", err.Error())
		return
	}

	data.ID = types.Int64Value(int64(key.ID))
	data.CreatedAt = types.StringValue(key.CreatedAt)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *SSHKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data SSHKeyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	key, err := r.client.GetSSHKey(ctx, int(data.ID.ValueInt64()))
	if err != nil {
		resp.Diagnostics.AddError("Failed to read SSH key", err.Error())
		return
	}

	data.Name = types.StringValue(key.Name)
	data.PublicKey = types.StringValue(key.PublicKey)
	data.CreatedAt = types.StringValue(key.CreatedAt)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *SSHKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// SSH keys are immutable on CloudBlast — delete and recreate
	var data SSHKeyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var oldData SSHKeyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &oldData)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// ForceNew behavior: if name or public_key changed, destroy+create
	if !data.Name.Equal(oldData.Name) || !data.PublicKey.Equal(oldData.PublicKey) {
		// Delete old key
		_ = r.client.DeleteSSHKey(ctx, int(oldData.ID.ValueInt64()))

		// Create new key
		key, err := r.client.CreateSSHKey(ctx, data.Name.ValueString(), data.PublicKey.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Failed to recreate SSH key", err.Error())
			return
		}
		data.ID = types.Int64Value(int64(key.ID))
		data.CreatedAt = types.StringValue(key.CreatedAt)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *SSHKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data SSHKeyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteSSHKey(ctx, int(data.ID.ValueInt64()))
	if err != nil {
		resp.Diagnostics.AddError("Failed to delete SSH key", err.Error())
	}
}

func (r *SSHKeyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	var id int
	if _, err := fmt.Sscanf(req.ID, "%d", &id); err != nil {
		resp.Diagnostics.AddError("Invalid import ID", "SSH key import ID must be a numeric ID.")
		return
	}

	key, err := r.client.GetSSHKey(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Failed to import SSH key", err.Error())
		return
	}

	data := SSHKeyResourceModel{
		ID:        types.Int64Value(int64(key.ID)),
		Name:      types.StringValue(key.Name),
		PublicKey: types.StringValue(key.PublicKey),
		CreatedAt: types.StringValue(key.CreatedAt),
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
