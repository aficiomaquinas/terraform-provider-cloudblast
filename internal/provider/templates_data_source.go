// Copyright 2026 aficiomaquinas
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &TemplatesDataSource{}

type TemplatesDataSource struct {
	client *Client
}

type TemplatesDataSourceModel struct {
	LocationID types.Int64     `tfsdk:"location_id"`
	Templates  []TemplateModel `tfsdk:"templates"`
}

type TemplateModel struct {
	Slug      types.String `tfsdk:"slug"`
	Name      types.String `tfsdk:"name"`
	GroupName types.String `tfsdk:"group_name"`
}

func NewTemplatesDataSource() datasource.DataSource {
	return &TemplatesDataSource{}
}

func (d *TemplatesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_templates"
}

func (d *TemplatesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists available OS templates for a CloudBlast location.",
		Attributes: map[string]schema.Attribute{
			"location_id": schema.Int64Attribute{
				Required:            true,
				MarkdownDescription: "Location ID to list templates for.",
			},
			"templates": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"slug":       schema.StringAttribute{Computed: true},
						"name":       schema.StringAttribute{Computed: true},
						"group_name": schema.StringAttribute{Computed: true},
					},
				},
			},
		},
	}
}

func (d *TemplatesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *TemplatesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data TemplatesDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	templates, err := d.client.ListTemplates(ctx, int(data.LocationID.ValueInt64()))
	if err != nil {
		resp.Diagnostics.AddError("Failed to list templates", err.Error())
		return
	}

	for _, t := range templates {
		data.Templates = append(data.Templates, TemplateModel{
			Slug:      types.StringValue(t.Slug),
			Name:      types.StringValue(t.Name),
			GroupName: types.StringValue(t.GroupName),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
