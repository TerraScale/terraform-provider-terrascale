package provider

import (
	"context"
	"errors"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	tb "terrascale.tech/sdk/v1"
)

type databaseResource struct{ clients *clients }
type databaseModel struct {
	ID             types.String `tfsdk:"id"`
	OrganizationID types.String `tfsdk:"organization_id"`
	ProjectID      types.String `tfsdk:"project_id"`
	Name           types.String `tfsdk:"name"`
	HomeRegion     types.String `tfsdk:"home_region"`
	ReplicaRegions types.Set    `tfsdk:"replica_regions"`
	AllRegions     types.Bool   `tfsdk:"all_regions"`
}

func (r *databaseResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_database"
}
func (r *databaseResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "A database. Name, parent or home region changes replace it and delete its data. Replicas must be cleared before deletion; the provider never removes replicas automatically.", Attributes: map[string]schema.Attribute{
		"id": stateID(), "organization_id": idAttribute("Owning organization ULID."), "project_id": idAttribute("Owning project ULID."),
		"name":            schema.StringAttribute{Required: true, Description: "Lowercase database name, up to 63 characters. Changes replace the database.", Validators: []validator.String{stringvalidator.RegexMatches(namePattern, "Use a lowercase name starting with a letter, followed by letters, digits, _ or -; maximum 63 characters.")}, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"home_region":     schema.StringAttribute{Optional: true, Computed: true, Description: "Region ID such as gru. Defaults to the API's region. Changes replace the database.", Validators: []validator.String{stringvalidator.RegexMatches(namePattern, "Use a region ID such as gru.")}, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown(), stringplanmodifier.RequiresReplace()}},
		"replica_regions": schema.SetAttribute{Optional: true, Computed: true, ElementType: types.StringType, Description: "Complete set of replica region IDs. Omit both placement controls to accept API placement on creation and preserve existing placement. Empty is supported for updates; creation requires replicas or all_regions=true."},
		"all_regions":     schema.BoolAttribute{Optional: true, Computed: true, Description: "Replicate to every serving region, including future regions. Omit both placement controls to accept API placement or preserve existing placement; an explicit replica_regions set selects listed placement. Cannot be combined with nonempty replica_regions."},
	}}
}
func (r *databaseResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	configure(req, resp, &r.clients)
}
func (r *databaseResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() || !req.Plan.Raw.IsKnown() {
		return
	}
	var config databaseModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if config.AllRegions.IsNull() && !config.ReplicaRegions.IsNull() && !config.ReplicaRegions.IsUnknown() {
		// Explicit replicas select listed placement even when computed state retains all_regions=true.
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("all_regions"), types.BoolValue(false))...)
	}
}
func (r *databaseResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var m databaseModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if m.AllRegions.ValueBool() && len(m.ReplicaRegions.Elements()) > 0 {
		resp.Diagnostics.AddError("Conflicting placement", "all_regions=true requires replica_regions to be empty.")
	}
	for _, v := range m.ReplicaRegions.Elements() {
		if s, ok := v.(types.String); ok && !s.IsUnknown() && (s.IsNull() || !namePattern.MatchString(s.ValueString())) {
			resp.Diagnostics.AddError("Invalid region", "replica_regions must contain region IDs such as gru.")
		}
	}
}
func (m *databaseModel) apply(ctx context.Context, p *tb.ManagedDatabase) {
	m.ID = types.StringValue(p.ID)
	m.Name = types.StringValue(p.Name)
	m.HomeRegion = types.StringPointerValue(p.HomeRegion)
	m.ReplicaRegions, _ = types.SetValueFrom(ctx, types.StringType, append([]string{}, p.ReplicaRegions...))
	m.AllRegions = types.BoolValue(p.AllRegions)
}
func (r *databaseResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m databaseModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || !requireManagement(r.clients, &resp.Diagnostics) {
		return
	}
	var regions []string
	if !m.ReplicaRegions.IsUnknown() && !m.ReplicaRegions.IsNull() {
		resp.Diagnostics.Append(m.ReplicaRegions.ElementsAs(ctx, &regions, false)...)
		if len(regions) == 0 && !m.AllRegions.ValueBool() {
			resp.Diagnostics.AddError("Empty creation placement", "Omit replica_regions to accept server placement, name at least one replica, or set all_regions=true. An explicit empty replica set is supported only for updates or all-region placement.")
		}
	}
	if m.AllRegions.ValueBool() && len(regions) > 0 {
		resp.Diagnostics.AddError("Conflicting placement", "all_regions=true requires replica_regions to be empty.")
	}
	if resp.Diagnostics.HasError() {
		return
	}
	if m.AllRegions.ValueBool() {
		// The public API rejects any nonnull replica list with all-region creation.
		regions = nil
	}
	db, err := m.database(ctx, r.clients)
	if err != nil {
		apiError(&resp.Diagnostics, "Initialize database client", err)
		return
	}
	p, err := db.CreateIn(ctx, tb.Placement{HomeRegion: m.HomeRegion.ValueString(), ReplicaRegions: regions, AllRegions: m.AllRegions.ValueBool()})
	if err != nil {
		apiError(&resp.Diagnostics, "Create database", err)
		return
	}
	m.apply(ctx, p)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *databaseResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m databaseModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || !requireManagement(r.clients, &resp.Diagnostics) {
		return
	}
	db, err := m.database(ctx, r.clients)
	var p *tb.ManagedDatabase
	if err == nil {
		p, err = db.Info(ctx)
	}
	if notFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apiError(&resp.Diagnostics, "Read database", err)
		return
	}
	if !m.ID.IsNull() && m.ID.ValueString() != p.ID {
		resp.State.RemoveResource(ctx)
		return
	}
	m.apply(ctx, p)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *databaseResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m databaseModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || !requireManagement(r.clients, &resp.Diagnostics) {
		return
	}
	var prior databaseModel
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}
	db, err := prior.database(ctx, r.clients)
	if err != nil {
		apiError(&resp.Diagnostics, "Read database before update", err)
		return
	}
	current, err := db.Info(ctx)
	if err != nil {
		apiError(&resp.Diagnostics, "Read database before update", err)
		return
	}
	if current.ID != prior.ID.ValueString() {
		resp.Diagnostics.AddError("Database identity changed", "The database at this name has a different ID. Refresh state before updating placement.")
		return
	}
	var p *tb.ManagedDatabase
	if m.AllRegions.ValueBool() {
		p, err = db.SetAllRegions(ctx)
	} else {
		replicas := m.ReplicaRegions
		if replicas.IsUnknown() || replicas.IsNull() {
			replicas = prior.ReplicaRegions
		}
		var regions []string
		resp.Diagnostics.Append(replicas.ElementsAs(ctx, &regions, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
		p, err = db.SetRegions(ctx, regions)
	}
	if err != nil {
		apiError(&resp.Diagnostics, "Update database regions", err)
		return
	}
	m.apply(ctx, p)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *databaseResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m databaseModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || !requireManagement(r.clients, &resp.Diagnostics) {
		return
	}
	db, err := m.database(ctx, r.clients)
	if notFound(err) {
		return
	}
	if err != nil {
		apiError(&resp.Diagnostics, "Read database before deletion", err)
		return
	}
	current, err := db.Info(ctx)
	if notFound(err) {
		return
	}
	if err != nil {
		apiError(&resp.Diagnostics, "Read database before deletion", err)
		return
	}
	if current.ID != m.ID.ValueString() {
		resp.Diagnostics.AddError("Database identity changed", "The database at this name has a different ID. Refresh state before deletion.")
		return
	}
	// shortcut: identity check and name-based delete are separate; use conditional delete when the SDK supports it.
	err = db.Delete(ctx)
	if err != nil && !notFound(err) {
		apiError(&resp.Diagnostics, "Delete database (clear replicas first)", err)
	}
}
func (r *databaseResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	setImport(ctx, req.ID, resp, "organization_id", "project_id", "resource_id")
}

func (m *databaseModel) database(ctx context.Context, c *clients) (*tb.Database, error) {
	client, err := c.project(m.OrganizationID.ValueString(), m.ProjectID.ValueString())
	if err != nil {
		return nil, err
	}
	if !m.Name.IsNull() && !m.Name.IsUnknown() {
		return client.Database(m.ProjectID.ValueString(), m.Name.ValueString()), nil
	}
	// Imports retain their ULID identity; public database routes require a name.
	seen := map[string]bool{}
	after := ""
	for {
		page, err := client.Databases(ctx, m.ProjectID.ValueString(), tb.PageOptions{Limit: 100, After: after})
		if err != nil {
			return nil, err
		}
		for _, db := range page.Items {
			if db.ID == m.ID.ValueString() {
				m.Name = types.StringValue(db.Name)
				return client.Database(m.ProjectID.ValueString(), db.Name), nil
			}
		}
		if page.Next == "" {
			return nil, &tb.APIError{Status: http.StatusNotFound, Code: tb.ErrUnknownDatabase}
		}
		if seen[page.Next] {
			return nil, errors.New("repeated database pagination cursor")
		}
		seen[page.Next] = true
		after = page.Next
	}
}
