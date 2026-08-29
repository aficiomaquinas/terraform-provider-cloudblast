// Copyright 2026 aficiomaquinas
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &LocationsDataSource{}

type LocationsDataSource struct {
	client *Client
}

type LocationsDataSourceModel struct {
	Locations []LocationModel `tfsdk:"locations"`
}

type LocationModel struct {
	ID          types.Int64  `tfsdk:"id"`
	ShortCode   types.String `tfsdk:"short_code"`
	Description types.String `tfsdk:"description"`
	OutOfStock  types.Bool   `tfsdk:"out_of_stock"`
}

func NewLocationsDataSource() datasource.DataSource {
	return &LocationsDataSource{}
}

func (d *LocationsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_locations"
}

func (d *LocationsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists all available CloudBlast data center locations.",
		Attributes: map[string]schema.Attribute{
			"locations": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":          schema.Int64Attribute{Computed: true},
						"short_code":  schema.StringAttribute{Computed: true},
						"description": schema.StringAttribute{Computed: true},
						"out_of_stock": schema.BoolAttribute{Computed: true},
					},
				},
			},
		},
	}
}

func (d *LocationsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *LocationsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data LocationsDataSourceModel

	locations, err := d.client.ListLocations(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list locations", err.Error())
		return
	}

	for _, l := range locations {
		data.Locations = append(data.Locations, LocationModel{
			ID:          types.Int64Value(int64(l.ID)),
			ShortCode:   types.StringValue(l.ShortCode),
			Description: types.StringValue(l.Description),
			OutOfStock:  types.BoolValue(l.OutOfStock),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
