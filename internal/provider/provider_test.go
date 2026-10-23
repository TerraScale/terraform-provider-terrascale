package provider

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	providerschema "github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"terrascale.tech/sdk/tsauth"
)

func dynamic(t *testing.T, state tfsdk.State) *tfprotov6.DynamicValue {
	t.Helper()
	v, err := tfprotov6.NewDynamicValue(state.Schema.Type().TerraformType(t.Context()), state.Raw)
	if err != nil {
		t.Fatal(err)
	}
	return &v
}
func protocolErrors(d []*tfprotov6.Diagnostic) bool {
	for _, v := range d {
		if v.Severity == tfprotov6.DiagnosticSeverityError {
			return true
		}
	}
	return false
}
func TestProviderMetadataAndSchema(t *testing.T) {
	p := New("0.0.1")()
	var metadata provider.MetadataResponse
	p.Metadata(t.Context(), provider.MetadataRequest{}, &metadata)
	if metadata.TypeName != "terrascale" || metadata.Version != "0.0.1" {
		t.Fatal("wrong provider metadata")
	}
	server := providerserver.NewProtocol6(p)()
	resp, err := server.GetProviderSchema(t.Context(), &tfprotov6.GetProviderSchemaRequest{})
	if err != nil || protocolErrors(resp.Diagnostics) {
		t.Fatalf("provider schema failed: %v %v", err, resp.Diagnostics)
	}
	if len(resp.ResourceSchemas) != 4 {
		t.Fatal("expected four resources")
	}
	for _, name := range []string{"project", "database", "collection", "api_key"} {
		if resp.ResourceSchemas["terrascale_"+name] == nil {
			t.Fatalf("missing resource %s", name)
		}
	}
	if !schemaFor(t, &apiKeyResource{}).Attributes["secret"].IsSensitive() {
		t.Fatal("key secret must be sensitive")
	}
}
func TestConfigureCredentials(t *testing.T) {
	for _, tc := range []struct {
		name, access, project string
		env, valid            bool
	}{
		{name: "management only", access: "fixture.user.signature", valid: true},
		{name: "collection only", project: "fixture.project.signature", valid: true},
		{name: "environment", access: "fixture.user.signature", project: "fixture.project.signature", env: true, valid: true},
		{name: "invalid user token", access: "fixture\nsecret", valid: false},
		{name: "invalid project token", project: "fixture-secret", valid: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TERRASCALE_ACCESS_TOKEN", "")
			t.Setenv("TERRASCALE_PROJECT_TOKEN", "")
			p := New("test")()
			var s provider.SchemaResponse
			p.Schema(t.Context(), provider.SchemaRequest{}, &s)
			var m providerModel
			if tc.env {
				t.Setenv("TERRASCALE_ACCESS_TOKEN", tc.access)
				t.Setenv("TERRASCALE_PROJECT_TOKEN", tc.project)
				m = providerModel{AccessToken: types.StringNull(), ProjectToken: types.StringNull()}
			} else {
				m = providerModel{AccessToken: types.StringValue(tc.access), ProjectToken: types.StringValue(tc.project)}
			}
			state := tfsdk.State{Schema: s.Schema}
			noErrors(t, state.Set(t.Context(), m))
			var resp provider.ConfigureResponse
			p.Configure(t.Context(), provider.ConfigureRequest{Config: tfsdk.Config{Schema: s.Schema, Raw: state.Raw}}, &resp)
			if resp.Diagnostics.HasError() == tc.valid {
				t.Fatalf("unexpected configure diagnostics: %v", resp.Diagnostics)
			}
			for _, d := range resp.Diagnostics {
				if strings.Contains(d.Detail(), "fixture-secret") {
					t.Fatal("credential leaked in diagnostic")
				}
			}
			if tc.valid {
				c, ok := resp.ResourceData.(*clients)
				if !ok {
					t.Fatal("missing clients")
				}
				if tc.access != "" {
					token, err := c.exchange.SubjectToken(t.Context())
					if err != nil || token != tc.access || c.exchange.ClientID != "ts-cli" || !slices.Equal(c.exchange.Scopes, []string{tsauth.ScopeDatabasesRead, tsauth.ScopeDatabasesWrite}) {
						t.Fatal("incorrect user exchange configuration")
					}
				}
				if (c.management != nil) != (tc.access != "") || (c.data != nil) != (tc.project != "") {
					t.Fatal("wrong credential routing")
				}
			}
		})
	}
	p := New("test")()
	var s provider.SchemaResponse
	p.Schema(t.Context(), provider.SchemaRequest{}, &s)
	if !s.Schema.Attributes["access_token"].(providerschema.StringAttribute).Sensitive || !s.Schema.Attributes["project_token"].IsSensitive() {
		t.Fatal("credentials must be sensitive")
	}
}
func TestResourceConfigValidation(t *testing.T) {
	server := providerserver.NewProtocol6(New("test")())()
	for _, tc := range []struct {
		name, kind string
		r          resource.Resource
		m          any
		valid      bool
	}{
		{"valid project", "project", &projectResource{}, projectModel{ID: types.StringNull(), OrganizationID: types.StringValue(orgID), Name: types.StringValue("Application")}, true},
		{"prefixed ID", "project", &projectResource{}, projectModel{ID: types.StringNull(), OrganizationID: types.StringValue("org_" + orgID), Name: types.StringValue("app")}, false},
		{"lowercase ID", "project", &projectResource{}, projectModel{ID: types.StringNull(), OrganizationID: types.StringValue(strings.ToLower(orgID)), Name: types.StringValue("app")}, false},
		{"trimmed display name", "project", &projectResource{}, projectModel{ID: types.StringNull(), OrganizationID: types.StringValue(orgID), Name: types.StringValue(" app ")}, false},
		{"control display name", "project", &projectResource{}, projectModel{ID: types.StringNull(), OrganizationID: types.StringValue(orgID), Name: types.StringValue("app\nname")}, false},
		{"empty display name", "project", &projectResource{}, projectModel{ID: types.StringNull(), OrganizationID: types.StringValue(orgID), Name: types.StringValue("")}, false},
		{"oversized display name", "project", &projectResource{}, projectModel{ID: types.StringNull(), OrganizationID: types.StringValue(orgID), Name: types.StringValue(strings.Repeat("x", 129))}, false},
		{"valid collection", "collection", &collectionResource{}, collectionModel{ID: types.StringNull(), ProjectID: types.StringValue(projectID), DatabaseName: types.StringValue("main"), Name: types.StringValue("users")}, true},
		{"unsafe collection name", "collection", &collectionResource{}, collectionModel{ID: types.StringNull(), ProjectID: types.StringValue(projectID), DatabaseName: types.StringValue("main"), Name: types.StringValue("../users")}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := server.ValidateResourceConfig(t.Context(), &tfprotov6.ValidateResourceConfigRequest{TypeName: "terrascale_" + tc.kind, Config: dynamic(t, stateFor(t, tc.r, tc.m))})
			if err != nil {
				t.Fatal(err)
			}
			if protocolErrors(resp.Diagnostics) == tc.valid {
				t.Fatalf("unexpected validation: %v", resp.Diagnostics)
			}
		})
	}
	for _, name := range []string{"main", "../main", ""} {
		m := keyState(t)
		m.ID = types.StringNull()
		m.Secret = types.StringNull()
		m.DatabaseName = types.StringValue(name)
		resp, err := server.ValidateResourceConfig(t.Context(), &tfprotov6.ValidateResourceConfigRequest{TypeName: "terrascale_api_key", Config: dynamic(t, stateFor(t, &apiKeyResource{}, m))})
		if err != nil || protocolErrors(resp.Diagnostics) == (name == "main") {
			t.Fatalf("incorrect database name validation: %v %v", err, resp.Diagnostics)
		}
	}
	for _, scopes := range [][]string{{"documents:read"}, {}, {"admin"}} {
		t.Run("scopes "+strings.Join(scopes, ","), func(t *testing.T) {
			m := keyState(t)
			m.ID = types.StringNull()
			m.Secret = types.StringNull()
			m.Scopes, _ = types.SetValueFrom(t.Context(), types.StringType, scopes)
			resp, err := server.ValidateResourceConfig(t.Context(), &tfprotov6.ValidateResourceConfigRequest{TypeName: "terrascale_api_key", Config: dynamic(t, stateFor(t, &apiKeyResource{}, m))})
			if err != nil {
				t.Fatal(err)
			}
			valid := len(scopes) == 1 && scopes[0] == "documents:read"
			if protocolErrors(resp.Diagnostics) == valid {
				t.Fatalf("unexpected scope validation: %v", resp.Diagnostics)
			}
		})
	}
	for _, tc := range []struct {
		name    string
		all     bool
		regions []string
		valid   bool
	}{{"default", false, []string{}, true}, {"all", true, []string{}, true}, {"explicit", false, []string{"fra-1"}, true}, {"conflict", true, []string{"fra-1"}, false}, {"invalid region", false, []string{"../region"}, false}} {
		t.Run(tc.name, func(t *testing.T) {
			m := databaseState()
			m.ID = types.StringNull()
			m.HomeRegion = types.StringNull()
			m.AllRegions = types.BoolValue(tc.all)
			m.ReplicaRegions, _ = types.SetValueFrom(t.Context(), types.StringType, tc.regions)
			resp, err := server.ValidateResourceConfig(t.Context(), &tfprotov6.ValidateResourceConfigRequest{TypeName: "terrascale_database", Config: dynamic(t, stateFor(t, &databaseResource{}, m))})
			if err != nil {
				t.Fatal(err)
			}
			if protocolErrors(resp.Diagnostics) == tc.valid {
				t.Fatalf("unexpected placement validation: %v", resp.Diagnostics)
			}
		})
	}
}
func TestReplacementPlanning(t *testing.T) {
	server := providerserver.NewProtocol6(New("test")())()
	for _, tc := range []struct {
		name, kind              string
		r                       resource.Resource
		prior, config, proposed any
		replace                 bool
	}{
		{"rename project", "project", &projectResource{}, projectState(), projectModel{ID: types.StringNull(), OrganizationID: types.StringValue(orgID), Name: types.StringValue("renamed")}, projectModel{ID: types.StringValue(projectID), OrganizationID: types.StringValue(orgID), Name: types.StringValue("renamed")}, false},
		{"rename collection", "collection", &collectionResource{}, collectionState(), collectionModel{ID: types.StringNull(), ProjectID: types.StringValue(projectID), DatabaseName: types.StringValue("main"), Name: types.StringValue("other")}, collectionModel{ID: types.StringValue(collectionID), ProjectID: types.StringValue(projectID), DatabaseName: types.StringValue("main"), Name: types.StringValue("other")}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := server.PlanResourceChange(t.Context(), &tfprotov6.PlanResourceChangeRequest{TypeName: "terrascale_" + tc.kind, PriorState: dynamic(t, stateFor(t, tc.r, tc.prior)), Config: dynamic(t, stateFor(t, tc.r, tc.config)), ProposedNewState: dynamic(t, stateFor(t, tc.r, tc.proposed))})
			if err != nil || protocolErrors(resp.Diagnostics) {
				t.Fatalf("plan failed: %v %v", err, resp.Diagnostics)
			}
			if (len(resp.RequiresReplace) > 0) != tc.replace {
				t.Fatalf("wrong replacement plan: %v", resp.RequiresReplace)
			}
		})
	}
	prior := keyState(t)
	config := prior
	config.Name = types.StringValue("rotated")
	config.ID = types.StringNull()
	config.Secret = types.StringNull()
	proposed := config
	proposed.ID = prior.ID
	proposed.Secret = prior.Secret
	resp, err := server.PlanResourceChange(t.Context(), &tfprotov6.PlanResourceChangeRequest{TypeName: "terrascale_api_key", PriorState: dynamic(t, stateFor(t, &apiKeyResource{}, prior)), Config: dynamic(t, stateFor(t, &apiKeyResource{}, config)), ProposedNewState: dynamic(t, stateFor(t, &apiKeyResource{}, proposed))})
	if err != nil || protocolErrors(resp.Diagnostics) || len(resp.RequiresReplace) == 0 {
		t.Fatalf("key replacement plan failed: %v %v", err, resp.Diagnostics)
	}
	raw, err := resp.PlannedState.Unmarshal(schemaFor(t, &apiKeyResource{}).Type().TerraformType(t.Context()))
	if err != nil {
		t.Fatal(err)
	}
	state := tfsdk.State{Schema: schemaFor(t, &apiKeyResource{}), Raw: raw}
	var m apiKeyModel
	noErrors(t, state.Get(t.Context(), &m))
	if !m.Secret.IsUnknown() {
		t.Fatal("key rotation reused old secret in plan")
	}
}

// SDK clients are injected only into this test provider; production always uses the public origin.
type placementTestProvider struct {
	provider.Provider
	clients *clients
}

func (p placementTestProvider) Configure(_ context.Context, _ provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	resp.ResourceData = p.clients
}
func placementServer(t *testing.T, c *clients) tfprotov6.ProviderServer {
	t.Helper()
	server := providerserver.NewProtocol6(placementTestProvider{Provider: New("test")(), clients: c})()
	resp, err := server.ConfigureProvider(t.Context(), &tfprotov6.ConfigureProviderRequest{Config: &tfprotov6.DynamicValue{JSON: []byte(`{"access_token":null,"project_token":null}`)}})
	if err != nil || protocolErrors(resp.Diagnostics) {
		t.Fatalf("configure failed: %v %v", err, resp.Diagnostics)
	}
	return server
}
func databaseConfig() databaseModel {
	m := databaseState()
	m.ID = types.StringNull()
	m.HomeRegion = types.StringNull()
	m.ReplicaRegions = types.SetNull(types.StringType)
	m.AllRegions = types.BoolNull()
	return m
}
func databaseProtocolState(t *testing.T, v *tfprotov6.DynamicValue) tfsdk.State {
	t.Helper()
	s := schemaFor(t, &databaseResource{})
	raw, err := v.Unmarshal(s.Type().TerraformType(t.Context()))
	if err != nil {
		t.Fatal(err)
	}
	return tfsdk.State{Schema: s, Raw: raw}
}
func TestDatabaseProtocolPlacement(t *testing.T) {
	for _, mode := range []string{"server-selected", "explicit replicas", "explicit replicas with omitted mode"} {
		t.Run(mode, func(t *testing.T) {
			explicit := mode != "server-selected"
			body := `{"name":"main","replicaRegions":null}`
			config := databaseConfig()
			if explicit {
				config.HomeRegion = types.StringValue("gru-1")
				config.ReplicaRegions, _ = types.SetValueFrom(t.Context(), types.StringType, []string{"fra-1"})
				if mode == "explicit replicas" {
					config.AllRegions = types.BoolValue(false)
				}
				body = `{"name":"main","homeRegion":"gru-1","replicaRegions":["fra-1"]}`
			}
			server := placementServer(t, fakeClients(t, step{method: "POST", path: databasesPath, body: body, response: databaseJSON(false, `["fra-1"]`), status: 201}))
			null := dynamic(t, emptyState(t, &databaseResource{}))
			cfg := dynamic(t, stateFor(t, &databaseResource{}, config))
			plan, err := server.PlanResourceChange(t.Context(), &tfprotov6.PlanResourceChangeRequest{TypeName: "terrascale_database", PriorState: null, Config: cfg, ProposedNewState: cfg})
			if err != nil || protocolErrors(plan.Diagnostics) {
				t.Fatalf("plan failed: %v %v", err, plan.Diagnostics)
			}
			var planned databaseModel
			noErrors(t, databaseProtocolState(t, plan.PlannedState).Get(t.Context(), &planned))
			if explicit {
				if !planned.ReplicaRegions.Equal(config.ReplicaRegions) || !planned.HomeRegion.Equal(config.HomeRegion) || !planned.AllRegions.Equal(types.BoolValue(false)) {
					t.Fatal("explicit placement changed during plan")
				}
			} else if !planned.ReplicaRegions.IsUnknown() || !planned.HomeRegion.IsUnknown() || !planned.AllRegions.IsUnknown() {
				t.Fatal("omitted placement must remain computed until creation")
			}
			applied, err := server.ApplyResourceChange(t.Context(), &tfprotov6.ApplyResourceChangeRequest{TypeName: "terrascale_database", PriorState: null, Config: cfg, PlannedState: plan.PlannedState})
			if err != nil || protocolErrors(applied.Diagnostics) {
				t.Fatalf("apply failed: %v %v", err, applied.Diagnostics)
			}
			state := databaseProtocolState(t, applied.NewState)
			var actual databaseModel
			noErrors(t, state.Get(t.Context(), &actual))
			if actual.HomeRegion.ValueString() != "gru-1" || len(actual.ReplicaRegions.Elements()) != 1 || actual.AllRegions.ValueBool() {
				t.Fatal("server-selected placement not retained")
			}
			next, err := server.PlanResourceChange(t.Context(), &tfprotov6.PlanResourceChangeRequest{TypeName: "terrascale_database", PriorState: applied.NewState, Config: cfg, ProposedNewState: applied.NewState})
			if err != nil || protocolErrors(next.Diagnostics) {
				t.Fatalf("subsequent plan failed: %v %v", err, next.Diagnostics)
			}
			if !databaseProtocolState(t, next.PlannedState).Raw.Equal(state.Raw) || len(next.RequiresReplace) != 0 {
				t.Fatal("subsequent plan changed server-selected or explicit placement")
			}
		})
	}
}
func TestDatabaseProtocolImportPreservesOmittedPlacement(t *testing.T) {
	for _, all := range []bool{false, true} {
		t.Run(map[bool]string{false: "listed", true: "all regions"}[all], func(t *testing.T) {
			regions := `["fra-1"]`
			if all {
				regions = `[]`
			}
			server := placementServer(t, fakeClients(t, step{method: "GET", path: databasesPath + "?limit=100", response: `{"items":[` + databaseJSON(all, regions) + `],"next":""}`}, step{method: "GET", path: databasePath, response: databaseJSON(all, regions)}))
			imported, err := server.ImportResourceState(t.Context(), &tfprotov6.ImportResourceStateRequest{TypeName: "terrascale_database", ID: orgID + "/" + projectID + "/" + databaseID})
			if err != nil || protocolErrors(imported.Diagnostics) || len(imported.ImportedResources) != 1 {
				t.Fatalf("import failed: %v %v", err, imported.Diagnostics)
			}
			read, err := server.ReadResource(t.Context(), &tfprotov6.ReadResourceRequest{TypeName: "terrascale_database", CurrentState: imported.ImportedResources[0].State})
			if err != nil || protocolErrors(read.Diagnostics) {
				t.Fatalf("refresh failed: %v %v", err, read.Diagnostics)
			}
			config := dynamic(t, stateFor(t, &databaseResource{}, databaseConfig()))
			plan, err := server.PlanResourceChange(t.Context(), &tfprotov6.PlanResourceChangeRequest{TypeName: "terrascale_database", PriorState: read.NewState, Config: config, ProposedNewState: read.NewState})
			if err != nil || protocolErrors(plan.Diagnostics) {
				t.Fatalf("import plan failed: %v %v", err, plan.Diagnostics)
			}
			if !databaseProtocolState(t, plan.PlannedState).Raw.Equal(databaseProtocolState(t, read.NewState).Raw) || len(plan.RequiresReplace) != 0 {
				t.Fatal("omitted configuration changed imported placement")
			}
		})
	}
}
func TestDatabaseProtocolPlacementUpdates(t *testing.T) {
	for _, tc := range []struct {
		name           string
		priorAll       bool
		priorRegions   string
		replicas       []string
		all            types.Bool
		body, response string
	}{
		{name: "enable all", priorRegions: `["fra-1"]`, all: types.BoolValue(true), body: `{"allRegions":true}`, response: databaseJSON(true, `[]`)},
		{name: "disable all", priorAll: true, priorRegions: `[]`, all: types.BoolValue(false), body: `{"replicaRegions":[]}`, response: databaseJSON(false, `[]`)},
		{name: "explicit list replaces all", priorAll: true, priorRegions: `[]`, replicas: []string{"fra-1"}, all: types.BoolNull(), body: `{"replicaRegions":["fra-1"]}`, response: databaseJSON(false, `["fra-1"]`)},
		{name: "explicit empty disables all", priorAll: true, priorRegions: `[]`, replicas: []string{}, all: types.BoolNull(), body: `{"replicaRegions":[]}`, response: databaseJSON(false, `[]`)},
		{name: "explicit empty clears replicas", priorRegions: `["fra-1"]`, replicas: []string{}, all: types.BoolNull(), body: `{"replicaRegions":[]}`, response: databaseJSON(false, `[]`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := fakeClients(t, step{method: "GET", path: databasePath, response: databaseJSON(tc.priorAll, tc.priorRegions)}, step{method: "PATCH", path: databasePath, body: tc.body, response: tc.response})
			server := placementServer(t, c)
			prior := databaseState()
			prior.AllRegions = types.BoolValue(tc.priorAll)
			var regions []string
			if err := json.Unmarshal([]byte(tc.priorRegions), &regions); err != nil {
				t.Fatal(err)
			}
			prior.ReplicaRegions, _ = types.SetValueFrom(t.Context(), types.StringType, regions)
			config := databaseConfig()
			config.AllRegions = tc.all
			proposed := prior
			if tc.replicas != nil {
				config.ReplicaRegions, _ = types.SetValueFrom(t.Context(), types.StringType, tc.replicas)
				proposed.ReplicaRegions = config.ReplicaRegions
			}
			if !tc.all.IsNull() {
				proposed.AllRegions = tc.all
			}
			priorValue := dynamic(t, stateFor(t, &databaseResource{}, prior))
			cfg := dynamic(t, stateFor(t, &databaseResource{}, config))
			plan, err := server.PlanResourceChange(t.Context(), &tfprotov6.PlanResourceChangeRequest{TypeName: "terrascale_database", PriorState: priorValue, Config: cfg, ProposedNewState: dynamic(t, stateFor(t, &databaseResource{}, proposed))})
			if err != nil || protocolErrors(plan.Diagnostics) || len(plan.RequiresReplace) != 0 {
				t.Fatalf("update plan failed: %v %v", err, plan.Diagnostics)
			}
			if tc.replicas != nil && tc.all.IsNull() {
				var planned databaseModel
				noErrors(t, databaseProtocolState(t, plan.PlannedState).Get(t.Context(), &planned))
				if !planned.AllRegions.Equal(types.BoolValue(false)) {
					t.Fatal("explicit replicas must plan all_regions=false")
				}
			}
			applied, err := server.ApplyResourceChange(t.Context(), &tfprotov6.ApplyResourceChangeRequest{TypeName: "terrascale_database", PriorState: priorValue, Config: cfg, PlannedState: plan.PlannedState})
			if err != nil || protocolErrors(applied.Diagnostics) {
				t.Fatalf("update apply failed: %v %v", err, applied.Diagnostics)
			}
			var actual databaseModel
			noErrors(t, databaseProtocolState(t, applied.NewState).Get(t.Context(), &actual))
			if !tc.all.IsNull() && !actual.AllRegions.Equal(tc.all) {
				t.Fatal("explicit all_regions setting lost")
			}
			if tc.replicas != nil && !actual.ReplicaRegions.Equal(config.ReplicaRegions) {
				t.Fatal("explicit replica list lost")
			}
		})
	}
}

func TestDatabaseProtocolUnresolvedPlacement(t *testing.T) {
	for _, tc := range []struct {
		name     string
		replicas types.Set
		all      types.Bool
		wantAll  types.Bool
	}{
		{"both omitted", types.SetNull(types.StringType), types.BoolNull(), types.BoolValue(true)},
		{"unknown replicas", types.SetUnknown(types.StringType), types.BoolNull(), types.BoolUnknown()},
		{"unknown mode", types.SetValueMust(types.StringType, nil), types.BoolUnknown(), types.BoolUnknown()},
		{"unknown replica element", types.SetValueMust(types.StringType, []attr.Value{types.StringUnknown()}), types.BoolNull(), types.BoolValue(false)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := placementServer(t, fakeClients(t))
			prior := databaseState()
			prior.AllRegions = types.BoolValue(true)
			config := databaseConfig()
			config.ReplicaRegions = tc.replicas
			config.AllRegions = tc.all
			proposed := prior
			if !tc.replicas.IsNull() {
				proposed.ReplicaRegions = tc.replicas
			}
			if !tc.all.IsNull() {
				proposed.AllRegions = tc.all
			}
			plan, err := server.PlanResourceChange(t.Context(), &tfprotov6.PlanResourceChangeRequest{TypeName: "terrascale_database", PriorState: dynamic(t, stateFor(t, &databaseResource{}, prior)), Config: dynamic(t, stateFor(t, &databaseResource{}, config)), ProposedNewState: dynamic(t, stateFor(t, &databaseResource{}, proposed))})
			if err != nil || protocolErrors(plan.Diagnostics) || len(plan.RequiresReplace) != 0 {
				t.Fatalf("placement plan failed: %v %v", err, plan.Diagnostics)
			}
			var planned databaseModel
			noErrors(t, databaseProtocolState(t, plan.PlannedState).Get(t.Context(), &planned))
			if !planned.AllRegions.Equal(tc.wantAll) || !planned.ReplicaRegions.Equal(proposed.ReplicaRegions) {
				t.Fatalf("unexpected placement plan: %+v", planned)
			}
		})
	}
}

func TestDatabaseHomeRegionChangeRequiresReplacement(t *testing.T) {
	server := providerserver.NewProtocol6(New("test")())()
	prior := databaseState()
	config := databaseConfig()
	config.HomeRegion = types.StringValue("fra-1")
	proposed := prior
	proposed.HomeRegion = config.HomeRegion
	resp, err := server.PlanResourceChange(t.Context(), &tfprotov6.PlanResourceChangeRequest{TypeName: "terrascale_database", PriorState: dynamic(t, stateFor(t, &databaseResource{}, prior)), Config: dynamic(t, stateFor(t, &databaseResource{}, config)), ProposedNewState: dynamic(t, stateFor(t, &databaseResource{}, proposed))})
	if err != nil || protocolErrors(resp.Diagnostics) || len(resp.RequiresReplace) != 1 {
		t.Fatalf("home-region replacement plan failed: %v %v", err, resp.Diagnostics)
	}
	if resp.RequiresReplace[0].String() != `AttributeName("home_region")` {
		t.Fatalf("unexpected replacement path: %s", resp.RequiresReplace[0])
	}
}
