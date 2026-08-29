package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &AccountDataSource{}

type AccountDataSource struct {
	client *Client
}

type AccountDataSourceModel struct {
	Name          types.String `tfsdk:"name"`
	Email         types.String `tfsdk:"email"`
	CreditBalance types.String `tfsdk:"credit_balance"`
}

func NewAccountDataSource() datasource.DataSource {
	return &AccountDataSource{}
}

func (d *AccountDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_account"
}

func (d *AccountDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Returns CloudBlast account information.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Account name.",
			},
			"email": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Account email.",
			},
			"credit_balance": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Current credit balance.",
			},
		},
	}
}

func (d *AccountDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected DataSource Configure Type", "Expected *Client")
		return
	}
	d.client = client
}

func (d *AccountDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data AccountDataSourceModel

	account, err := d.client.GetAccount(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to get account", err.Error())
		return
	}

	data.Name = types.StringValue(account.Name)
	data.Email = types.StringValue(account.Email)
	data.CreditBalance = types.StringValue(account.CreditBalance)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
