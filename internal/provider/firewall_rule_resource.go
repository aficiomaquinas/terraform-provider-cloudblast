package provider

import (
	"context"
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &FirewallRuleResource{}

type FirewallRuleResource struct {
	client *Client
}

type FirewallRuleResourceModel struct {
	ID              types.String `tfsdk:"id"`
	SecurityGroupID types.Int64  `tfsdk:"security_group_id"`
	Type            types.String `tfsdk:"type"`
	Action          types.String `tfsdk:"action"`
	Protocol        types.String `tfsdk:"protocol"`
	Source          types.String `tfsdk:"source"`
	Destination     types.String `tfsdk:"destination"`
	SourcePort      types.String `tfsdk:"source_port"`
	DestinationPort types.String `tfsdk:"destination_port"`
	Comment         types.String `tfsdk:"comment"`
	Priority        types.Int64  `tfsdk:"priority"`
}

func NewFirewallRuleResource() resource.Resource {
	return &FirewallRuleResource{}
}

func (r *FirewallRuleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_firewall_rule"
}

func (r *FirewallRuleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a CloudBlast firewall rule within a security group.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Rule ID (composite: groupID-ruleID).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"security_group_id": schema.Int64Attribute{
				Required:            true,
				MarkdownDescription: "Security group ID to add this rule to.",
			},
			"type": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Rule direction: `inbound` or `outbound`.",
			},
			"action": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Rule action: `ACCEPT`, `REJECT`, or `DROP`.",
			},
			"protocol": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Protocol: `tcp`, `udp`, `icmp`, etc.",
			},
			"source": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Source IP/CIDR for inbound rules.",
			},
			"destination": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Destination IP/CIDR for outbound rules.",
			},
			"source_port": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Source port or range.",
			},
			"destination_port": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Destination port or range (e.g., `80`, `8000:9000`).",
			},
			"comment": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Rule description/comment.",
			},
			"priority": schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "Rule priority (lower = higher priority).",
			},
		},
	}
}

func (r *FirewallRuleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *FirewallRuleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data FirewallRuleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	params := CreateFirewallRuleParams{
		Type:            data.Type.ValueString(),
		Action:          data.Action.ValueString(),
		Protocol:        data.Protocol.ValueString(),
		Source:          data.Source.ValueString(),
		Destination:     data.Destination.ValueString(),
		SourcePort:      data.SourcePort.ValueString(),
		DestinationPort: data.DestinationPort.ValueString(),
		Comment:         data.Comment.ValueString(),
		Priority:        int(data.Priority.ValueInt64()),
	}

	rule, err := r.client.CreateFirewallRule(ctx, int(data.SecurityGroupID.ValueInt64()), params)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create firewall rule", err.Error())
		return
	}

	// Composite ID: groupID-ruleID
	data.ID = types.StringValue(fmt.Sprintf("%d-%d", data.SecurityGroupID.ValueInt64(), rule.ID))

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *FirewallRuleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data FirewallRuleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	groupID, _, err := parseCompositeID(data.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid rule ID", err.Error())
		return
	}

	// Verify the security group still exists
	_, err = r.client.GetSecurityGroup(ctx, groupID)
	if err != nil {
		// SG deleted — rule is gone, remove from state
		resp.State.RemoveResource(ctx)
		return
	}

	// The CloudBlast API does not return individual firewall rules
	// in the GET /security-groups/{id} response. We trust the state
	// that was set during Create. If the rule was deleted outside
	// Terraform, this won't detect it — acceptable for this API.
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *FirewallRuleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Firewall rules are not updatable on CloudBlast — force recreation
	var data FirewallRuleResourceModel
	resp.Diagnostics.Append(req.State.Set(ctx, &data)...)
}

func (r *FirewallRuleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data FirewallRuleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	groupID, ruleID, err := parseCompositeID(data.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid rule ID", err.Error())
		return
	}

	err = r.client.DeleteFirewallRule(ctx, groupID, ruleID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to delete firewall rule", err.Error())
	}
}

// parseCompositeID parses "groupID-ruleID" format.
func parseCompositeID(id string) (groupID, ruleID int, err error) {
	var g, r int64
	n, scanErr := fmt.Sscanf(id, "%d-%d", &g, &r)
	if scanErr != nil || n != 2 {
		return 0, 0, fmt.Errorf("invalid composite ID format %q, expected groupID-ruleID", id)
	}
	groupID, _ = strconv.Atoi(strconv.FormatInt(g, 10))
	ruleID, _ = strconv.Atoi(strconv.FormatInt(r, 10))
	return groupID, ruleID, nil
}
