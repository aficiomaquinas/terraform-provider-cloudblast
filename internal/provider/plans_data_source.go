// Copyright 2026 aficiomaquinas
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &PlansDataSource{}

type PlansDataSource struct {
	client *Client
}

type PlansDataSourceModel struct {
	Plans []PlanModel `tfsdk:"plans"`
}

type PlanModel struct {
	ID           types.Int64   `tfsdk:"id"`
	Name         types.String  `tfsdk:"name"`
	CPU          types.Int64   `tfsdk:"cpu"`
	Memory       types.Int64   `tfsdk:"memory"`
	Disk         types.Int64   `tfsdk:"disk"`
	MonthlyPrice types.Float64 `tfsdk:"monthly_price"`
	HourlyPrice  types.Float64 `tfsdk:"hourly_price"`
}

func NewPlansDataSource() datasource.DataSource {
	return &PlansDataSource{}
}

func (d *PlansDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_plans"
}

func (d *PlansDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists all available CloudBlast server plans.",
		Attributes: map[string]schema.Attribute{
			"plans": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "List of available plans.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":            schema.Int64Attribute{Computed: true},
						"name":          schema.StringAttribute{Computed: true},
						"cpu":           schema.Int64Attribute{Computed: true},
						"memory":        schema.Int64Attribute{Computed: true, MarkdownDescription: "Memory in bytes."},
						"disk":          schema.Int64Attribute{Computed: true, MarkdownDescription: "Disk in bytes."},
						"monthly_price": schema.Float64Attribute{Computed: true},
						"hourly_price":  schema.Float64Attribute{Computed: true},
					},
				},
			},
		},
	}
}

func (d *PlansDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *PlansDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data PlansDataSourceModel

	plans, err := d.client.ListPlans(ctx, 0)
	if err != nil {
		resp.Diagnostics.AddError("Failed to list plans", err.Error())
		return
	}

	for _, p := range plans {
		data.Plans = append(data.Plans, PlanModel{
			ID:           types.Int64Value(int64(p.ID)),
			Name:         types.StringValue(p.Name),
			CPU:          types.Int64Value(int64(p.CPU)),
			Memory:       types.Int64Value(p.Memory),
			Disk:         types.Int64Value(p.Disk),
			MonthlyPrice: types.Float64Value(p.MonthlyPrice),
			HourlyPrice:  types.Float64Value(p.HourlyPrice),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
