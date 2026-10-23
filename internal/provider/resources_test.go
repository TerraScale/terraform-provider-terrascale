package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"terrascale.tech/sdk/tsauth"
	tb "terrascale.tech/sdk/v1"
)

const (
	orgID          = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	projectID      = "01ARZ3NDEKTSV4RRFFQ69G5FAW"
	databaseID     = "01ARZ3NDEKTSV4RRFFQ69G5FAX"
	collectionID   = "01ARZ3NDEKTSV4RRFFQ69G5FAY"
	keyID          = "01ARZ3NDEKTSV4RRFFQ69G5FAZ"
	managementPath = "/v1/terrascale/organizations/" + orgID + "/projects"
	projectPath    = managementPath + "/" + projectID
	databasesPath  = "/v1/projects/" + projectID + "/databases"
	databasePath   = databasesPath + "/main"
	collectionPath = "/v1/projects/" + projectID + "/databases/main/collections/users"
)

type step struct {
	method, path, body, response string
	status                       int
}

func fakeClients(t *testing.T, steps ...step) *clients {
	t.Helper()
	var mu sync.Mutex
	index := 0
	exchanges := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if req.URL.Path == "/terrascale/oauth/token" {
			exchanges++
			if exchanges > 1 {
				t.Error("project exchange source was not reused")
			}
			if err := req.ParseForm(); err != nil {
				t.Error(err)
			}
			want := map[string]string{"client_id": "00000000000000000000000007", "grant_type": "urn:ietf:params:oauth:grant-type:token-exchange", "subject_token_type": "urn:ietf:params:oauth:token-type:access_token", "subject_token": "fixture.user.signature", "organization": orgID, "project": projectID, "scope": "databases:read databases:write"}
			if req.Method != "POST" || len(req.Form) != len(want) {
				t.Error("incorrect token exchange request")
			}
			for field, value := range want {
				if req.Form.Get(field) != value {
					t.Errorf("incorrect exchange field %s", field)
				}
			}
			w.Header().Set("Content-Type", "application/json")
			if index < len(steps) && steps[index].path == req.URL.Path {
				s := steps[index]
				index++
				w.WriteHeader(s.status)
				_, _ = io.WriteString(w, s.response)
				return
			}
			_, _ = io.WriteString(w, `{"access_token":"fixture.exchanged.signature","token_type":"Bearer","expires_in":3600}`)
			return
		}
		if index >= len(steps) {
			t.Errorf("unexpected request: %s %s", req.Method, req.URL.Path)
			w.WriteHeader(500)
			return
		}
		s := steps[index]
		index++
		if req.Method != s.method || req.URL.RequestURI() != s.path {
			t.Errorf("request %d: got %s %s, want %s %s", index, req.Method, req.URL.RequestURI(), s.method, s.path)
		}
		authorization := "Bearer fixture.user.signature"
		if strings.HasPrefix(req.URL.Path, databasesPath) {
			authorization = "Bearer fixture.exchanged.signature"
			if strings.Contains(req.URL.Path, "/collections") {
				authorization = "Bearer fixture.project.signature"
			}
		}
		if req.Header.Get("Authorization") != authorization {
			t.Error("incorrect authorization for public route")
		}
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Error(err)
		}
		if s.body != "" {
			var got, want any
			if err := json.Unmarshal(body, &got); err != nil {
				t.Error(err)
			}
			if err := json.Unmarshal([]byte(s.body), &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("request %d body: got %s, want %s", index, body, s.body)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		status := s.status
		if status == 0 {
			status = 200
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, s.response)
	}))
	t.Cleanup(func() {
		server.Close()
		mu.Lock()
		defer mu.Unlock()
		if index != len(steps) {
			t.Errorf("sent %d requests, want %d", index, len(steps))
		}
	})
	m, err := tsauth.New(tsauth.Config{Issuer: server.URL + "/terrascale", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	c, err := tb.New(tb.Config{BaseURL: server.URL, Token: "fixture.project.signature", HTTPClient: server.Client(), MaxRetries: -1})
	if err != nil {
		t.Fatal(err)
	}
	return &clients{management: m.WithToken("fixture.user.signature"), data: c,
		exchange:       tsauth.ExchangeConfig{Issuer: server.URL + "/terrascale", HTTPClient: server.Client(), ClientID: cliClientID, SubjectToken: func(context.Context) (string, error) { return "fixture.user.signature", nil }, Scopes: []string{tsauth.ScopeDatabasesRead, tsauth.ScopeDatabasesWrite}},
		databaseConfig: tb.Config{BaseURL: server.URL, HTTPClient: server.Client(), MaxRetries: -1},
	}
}
func schemaFor(t *testing.T, r resource.Resource) schema.Schema {
	t.Helper()
	var resp resource.SchemaResponse
	r.Schema(t.Context(), resource.SchemaRequest{}, &resp)
	noErrors(t, resp.Diagnostics)
	noErrors(t, resp.Schema.ValidateImplementation(t.Context()))
	return resp.Schema
}
func stateFor(t *testing.T, r resource.Resource, model any) tfsdk.State {
	t.Helper()
	state := tfsdk.State{Schema: schemaFor(t, r)}
	noErrors(t, state.Set(t.Context(), model))
	return state
}
func emptyState(t *testing.T, r resource.Resource) tfsdk.State {
	t.Helper()
	s := schemaFor(t, r)
	return tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(t.Context()), nil)}
}
func planFor(t *testing.T, r resource.Resource, model any) tfsdk.Plan {
	t.Helper()
	state := stateFor(t, r, model)
	return tfsdk.Plan{Schema: state.Schema, Raw: state.Raw}
}
func noErrors(t *testing.T, d diag.Diagnostics) {
	t.Helper()
	if d.HasError() {
		t.Fatalf("diagnostics: %v", d)
	}
}
func projectJSON(name string) string {
	return `{"id":"` + projectID + `","organization_id":"` + orgID + `","created_by":"` + collectionID + `","name":"` + name + `"}`
}
func databaseJSON(all bool, regions string) string {
	return `{"id":"` + databaseID + `","projectId":"` + projectID + `","name":"main","homeRegion":"gru-1","replicaRegions":` + regions + `,"allRegions":` + map[bool]string{true: "true", false: "false"}[all] + `}`
}
func collectionJSON(id string) string { return `{"id":"` + id + `","name":"users","created_at":"1"}` }
func keyJSON() string {
	return `{"id":"` + keyID + `","organization_id":"` + orgID + `","project_id":"` + projectID + `","created_by":"` + collectionID + `","name":"reader","scopes":["documents:read"],"database":"main"}`
}
func projectState() projectModel {
	return projectModel{ID: types.StringValue(projectID), OrganizationID: types.StringValue(orgID), Name: types.StringValue("app")}
}
func databaseState() databaseModel {
	return databaseModel{ID: types.StringValue(databaseID), OrganizationID: types.StringValue(orgID), ProjectID: types.StringValue(projectID), Name: types.StringValue("main"), HomeRegion: types.StringValue("gru-1"), ReplicaRegions: types.SetValueMust(types.StringType, nil), AllRegions: types.BoolValue(false)}
}
func collectionState() collectionModel {
	return collectionModel{ID: types.StringValue(collectionID), ProjectID: types.StringValue(projectID), DatabaseName: types.StringValue("main"), Name: types.StringValue("users")}
}
func keyState(t *testing.T) apiKeyModel {
	t.Helper()
	scopes, d := types.SetValueFrom(t.Context(), types.StringType, []string{"documents:read"})
	noErrors(t, d)
	return apiKeyModel{ID: types.StringValue(keyID), OrganizationID: types.StringValue(orgID), ProjectID: types.StringValue(projectID), Name: types.StringValue("reader"), DatabaseName: types.StringValue("main"), Scopes: scopes, Secret: types.StringValue("fixture-secret")}
}

func TestProjectLifecycle(t *testing.T) {
	r := &projectResource{clients: fakeClients(t,
		step{method: "POST", path: managementPath, body: `{"name":"app"}`, response: projectJSON("app"), status: 201},
		step{method: "GET", path: projectPath, response: projectJSON("app")},
		step{method: "PATCH", path: projectPath, body: `{"name":"renamed"}`, response: projectJSON("renamed")},
		step{method: "DELETE", path: projectPath, status: 204},
	)}
	m := projectState()
	m.ID = types.StringUnknown()
	create := resource.CreateResponse{State: tfsdk.State{Schema: schemaFor(t, r)}}
	r.Create(t.Context(), resource.CreateRequest{Plan: planFor(t, r, m)}, &create)
	noErrors(t, create.Diagnostics)
	var created projectModel
	noErrors(t, create.State.Get(t.Context(), &created))
	if created.ID.ValueString() != projectID {
		t.Fatal("project ID not saved")
	}
	read := resource.ReadResponse{State: create.State}
	r.Read(t.Context(), resource.ReadRequest{State: create.State}, &read)
	noErrors(t, read.Diagnostics)
	created.Name = types.StringValue("renamed")
	update := resource.UpdateResponse{State: read.State}
	r.Update(t.Context(), resource.UpdateRequest{Plan: planFor(t, r, created), State: read.State}, &update)
	noErrors(t, update.Diagnostics)
	var updated projectModel
	noErrors(t, update.State.Get(t.Context(), &updated))
	if updated.Name.ValueString() != "renamed" {
		t.Fatal("rename not saved")
	}
	var deleted resource.DeleteResponse
	r.Delete(t.Context(), resource.DeleteRequest{State: update.State}, &deleted)
	noErrors(t, deleted.Diagnostics)
}
func TestDatabaseLifecycle(t *testing.T) {
	r := &databaseResource{clients: fakeClients(t,
		step{method: "POST", path: databasesPath, body: `{"name":"main","homeRegion":"gru-1","replicaRegions":["fra-1"]}`, response: databaseJSON(false, `["fra-1"]`), status: 201},
		step{method: "GET", path: databasePath, response: databaseJSON(false, `["fra-1"]`)},
		step{method: "GET", path: databasePath, response: databaseJSON(false, `["fra-1"]`)},
		step{method: "PATCH", path: databasePath, body: `{"allRegions":true}`, response: databaseJSON(true, `[]`)},
		step{method: "GET", path: databasePath, response: databaseJSON(true, `[]`)},
		step{method: "PATCH", path: databasePath, body: `{"replicaRegions":["fra-1"]}`, response: databaseJSON(false, `["fra-1"]`)},
		step{method: "GET", path: databasePath, response: databaseJSON(false, `["fra-1"]`)},
		step{method: "PATCH", path: databasePath, body: `{"replicaRegions":[]}`, response: databaseJSON(false, `[]`)},
		step{method: "GET", path: databasePath, response: databaseJSON(false, `[]`)},
		step{method: "DELETE", path: databasePath, status: 204},
	)}
	m := databaseState()
	m.ID = types.StringUnknown()
	m.ReplicaRegions, _ = types.SetValueFrom(t.Context(), types.StringType, []string{"fra-1"})
	create := resource.CreateResponse{State: tfsdk.State{Schema: schemaFor(t, r)}}
	r.Create(t.Context(), resource.CreateRequest{Plan: planFor(t, r, m)}, &create)
	noErrors(t, create.Diagnostics)
	read := resource.ReadResponse{State: create.State}
	r.Read(t.Context(), resource.ReadRequest{State: create.State}, &read)
	noErrors(t, read.Diagnostics)
	noErrors(t, read.State.Get(t.Context(), &m))
	if m.ReplicaRegions.IsNull() || m.HomeRegion.ValueString() != "gru-1" {
		t.Fatal("placement not saved")
	}
	m.AllRegions = types.BoolValue(true)
	update := resource.UpdateResponse{State: read.State}
	r.Update(t.Context(), resource.UpdateRequest{Plan: planFor(t, r, m), State: read.State}, &update)
	noErrors(t, update.Diagnostics)
	noErrors(t, update.State.Get(t.Context(), &m))
	if !m.AllRegions.ValueBool() {
		t.Fatal("all regions not saved")
	}
	m.AllRegions = types.BoolValue(false)
	m.ReplicaRegions, _ = types.SetValueFrom(t.Context(), types.StringType, []string{"fra-1"})
	r.Update(t.Context(), resource.UpdateRequest{Plan: planFor(t, r, m), State: update.State}, &update)
	noErrors(t, update.Diagnostics)
	noErrors(t, update.State.Get(t.Context(), &m))
	if len(m.ReplicaRegions.Elements()) != 1 {
		t.Fatal("replicas not saved")
	}
	m.ReplicaRegions = types.SetValueMust(types.StringType, nil)
	r.Update(t.Context(), resource.UpdateRequest{Plan: planFor(t, r, m), State: update.State}, &update)
	noErrors(t, update.Diagnostics)
	var deleted resource.DeleteResponse
	r.Delete(t.Context(), resource.DeleteRequest{State: update.State}, &deleted)
	noErrors(t, deleted.Diagnostics)
}
func TestCollectionLifecycle(t *testing.T) {
	r := &collectionResource{clients: fakeClients(t,
		step{method: "POST", path: strings.TrimSuffix(collectionPath, "/users"), body: `{"name":"users"}`, response: collectionJSON(collectionID), status: 201},
		step{method: "GET", path: collectionPath, response: collectionJSON(collectionID)},
		step{method: "GET", path: collectionPath, response: collectionJSON(collectionID)},
		step{method: "DELETE", path: collectionPath, response: collectionJSON(collectionID)},
	)}
	m := collectionState()
	m.ID = types.StringUnknown()
	create := resource.CreateResponse{State: tfsdk.State{Schema: schemaFor(t, r)}}
	r.Create(t.Context(), resource.CreateRequest{Plan: planFor(t, r, m)}, &create)
	noErrors(t, create.Diagnostics)
	read := resource.ReadResponse{State: create.State}
	r.Read(t.Context(), resource.ReadRequest{State: create.State}, &read)
	noErrors(t, read.Diagnostics)
	noErrors(t, read.State.Get(t.Context(), &m))
	if m.ID.ValueString() != collectionID {
		t.Fatal("collection ID not saved")
	}
	var deleted resource.DeleteResponse
	r.Delete(t.Context(), resource.DeleteRequest{State: read.State}, &deleted)
	noErrors(t, deleted.Diagnostics)
}
func TestKeyLifecyclePreservesSecretAcrossPaginatedRefresh(t *testing.T) {
	r := &apiKeyResource{clients: fakeClients(t,
		step{method: "POST", path: projectPath + "/api-keys", body: `{"name":"reader","scopes":["documents:read"],"database":"main"}`, response: `{"api_key":` + keyJSON() + `,"key":"fixture-secret"}`, status: 201},
		step{method: "GET", path: projectPath + "/api-keys?limit=100", response: `{"items":[],"next":"` + projectID + `"}`},
		step{method: "GET", path: projectPath + "/api-keys?after=" + projectID + "&limit=100", response: `{"items":[` + keyJSON() + `],"next":""}`},
		step{method: "DELETE", path: projectPath + "/api-keys/" + keyID, status: 204},
	)}
	m := keyState(t)
	m.ID = types.StringUnknown()
	m.Secret = types.StringUnknown()
	create := resource.CreateResponse{State: tfsdk.State{Schema: schemaFor(t, r)}}
	r.Create(t.Context(), resource.CreateRequest{Plan: planFor(t, r, m)}, &create)
	noErrors(t, create.Diagnostics)
	if create.Diagnostics.WarningsCount() != 1 {
		t.Fatal("missing state secret warning")
	}
	read := resource.ReadResponse{State: create.State}
	r.Read(t.Context(), resource.ReadRequest{State: create.State}, &read)
	noErrors(t, read.Diagnostics)
	noErrors(t, read.State.Get(t.Context(), &m))
	if m.Secret.ValueString() != "fixture-secret" || m.ID.ValueString() != keyID || m.DatabaseName.ValueString() != "main" {
		t.Fatal("refresh lost secret or ID")
	}
	var deleted resource.DeleteResponse
	r.Delete(t.Context(), resource.DeleteRequest{State: read.State}, &deleted)
	noErrors(t, deleted.Diagnostics)
}
func TestReadMissingRemovesStateAndForbiddenPreservesState(t *testing.T) {
	for _, kind := range []string{"project", "database", "collection", "api_key"} {
		for _, status := range []int{404, 403} {
			t.Run(kind+http.StatusText(status), func(t *testing.T) {
				route := map[string]string{"project": projectPath, "database": databasePath, "collection": collectionPath, "api_key": projectPath + "/api-keys?limit=100"}[kind]
				c := fakeClients(t, step{method: "GET", path: route, status: status, response: `{"error":{"code":"unknown_route","message":"fixture-secret"}}`})
				var r resource.Resource
				var m any
				switch kind {
				case "project":
					r = &projectResource{clients: c}
					m = projectState()
				case "database":
					r = &databaseResource{clients: c}
					m = databaseState()
				case "collection":
					r = &collectionResource{clients: c}
					m = collectionState()
				case "api_key":
					r = &apiKeyResource{clients: c}
					m = keyState(t)
				}
				state := stateFor(t, r, m)
				resp := resource.ReadResponse{State: state}
				r.Read(t.Context(), resource.ReadRequest{State: state}, &resp)
				if status == 404 {
					noErrors(t, resp.Diagnostics)
					if !resp.State.Raw.IsNull() {
						t.Fatal("missing resource still in state")
					}
				} else {
					if !resp.Diagnostics.HasError() || resp.State.Raw.IsNull() {
						t.Fatal("permission error removed state or was ignored")
					}
					for _, d := range resp.Diagnostics {
						if strings.Contains(d.Detail(), "fixture-secret") {
							t.Fatal("diagnostic exposes response message")
						}
					}
				}
			})
		}
	}
}
func TestDeleteMissingIsIdempotent(t *testing.T) {
	for _, kind := range []string{"project", "database", "collection", "api_key"} {
		t.Run(kind, func(t *testing.T) {
			method := "DELETE"
			route := map[string]string{"project": projectPath, "database": databasePath, "collection": collectionPath, "api_key": projectPath + "/api-keys/" + keyID}[kind]
			if kind == "collection" || kind == "database" {
				method = "GET"
			}
			c := fakeClients(t, step{method: method, path: route, status: 404, response: `{"error":{"code":"not_found"}}`})
			var r resource.Resource
			var m any
			switch kind {
			case "project":
				r = &projectResource{clients: c}
				m = projectState()
			case "database":
				r = &databaseResource{clients: c}
				m = databaseState()
			case "collection":
				r = &collectionResource{clients: c}
				m = collectionState()
			case "api_key":
				r = &apiKeyResource{clients: c}
				m = keyState(t)
			}
			var resp resource.DeleteResponse
			r.Delete(t.Context(), resource.DeleteRequest{State: stateFor(t, r, m)}, &resp)
			noErrors(t, resp.Diagnostics)
		})
	}
}
func TestCollectionDoesNotAdoptOrDeleteRecreatedName(t *testing.T) {
	for _, operation := range []string{"read", "delete"} {
		t.Run(operation, func(t *testing.T) {
			r := &collectionResource{clients: fakeClients(t, step{method: "GET", path: collectionPath, response: collectionJSON(keyID)})}
			state := stateFor(t, r, collectionState())
			if operation == "read" {
				resp := resource.ReadResponse{State: state}
				r.Read(t.Context(), resource.ReadRequest{State: state}, &resp)
				noErrors(t, resp.Diagnostics)
				if !resp.State.Raw.IsNull() {
					t.Fatal("adopted recreated collection")
				}
			} else {
				var resp resource.DeleteResponse
				r.Delete(t.Context(), resource.DeleteRequest{State: state}, &resp)
				if !resp.Diagnostics.HasError() {
					t.Fatal("ignored changed identity")
				}
			}
		})
	}
}
func TestKeyAbsentAndBrokenPagination(t *testing.T) {
	for _, repeated := range []bool{false, true} {
		t.Run(map[bool]string{false: "absent", true: "repeated cursor"}[repeated], func(t *testing.T) {
			steps := []step{{method: "GET", path: projectPath + "/api-keys?limit=100", response: `{"items":[],"next":""}`}}
			if repeated {
				steps[0].response = `{"items":[],"next":"` + projectID + `"}`
				steps = append(steps, step{method: "GET", path: projectPath + "/api-keys?after=" + projectID + "&limit=100", response: steps[0].response})
			}
			r := &apiKeyResource{clients: fakeClients(t, steps...)}
			state := stateFor(t, r, keyState(t))
			resp := resource.ReadResponse{State: state}
			r.Read(t.Context(), resource.ReadRequest{State: state}, &resp)
			if repeated {
				if !resp.Diagnostics.HasError() || resp.State.Raw.IsNull() {
					t.Fatal("broken pagination removed state or did not fail")
				}
			} else {
				noErrors(t, resp.Diagnostics)
				if !resp.State.Raw.IsNull() {
					t.Fatal("revoked key still in state")
				}
			}
		})
	}
}
func TestCanceledRequestAndMissingCredentials(t *testing.T) {
	r := &projectResource{clients: fakeClients(t)}
	state := stateFor(t, r, projectState())
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	resp := resource.ReadResponse{State: state}
	r.Read(ctx, resource.ReadRequest{State: state}, &resp)
	if !resp.Diagnostics.HasError() || resp.State.Raw.IsNull() {
		t.Fatal("cancellation did not preserve state with error")
	}
	r.clients = &clients{}
	resp = resource.ReadResponse{State: state}
	r.Read(t.Context(), resource.ReadRequest{State: state}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("missing management credential not diagnosed")
	}
	col := &collectionResource{clients: &clients{}}
	cs := stateFor(t, col, collectionState())
	cr := resource.ReadResponse{State: cs}
	col.Read(t.Context(), resource.ReadRequest{State: cs}, &cr)
	if !cr.Diagnostics.HasError() {
		t.Fatal("missing project credential not diagnosed")
	}
}
func TestImports(t *testing.T) {
	for _, tc := range []struct {
		name, id    string
		r           resource.Resource
		field, want string
	}{
		{"project", orgID + "/" + projectID, &projectResource{}, "id", projectID},
		{"database", orgID + "/" + projectID + "/" + databaseID, &databaseResource{}, "id", databaseID},
		{"collection", projectID + "/main/users", &collectionResource{}, "name", "users"},
		{"api_key", orgID + "/" + projectID + "/" + keyID, &apiKeyResource{}, "id", keyID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := resource.ImportStateResponse{State: emptyState(t, tc.r)}
			tc.r.(resource.ResourceWithImportState).ImportState(t.Context(), resource.ImportStateRequest{ID: tc.id}, &resp)
			noErrors(t, resp.Diagnostics)
			var got types.String
			noErrors(t, resp.State.GetAttribute(t.Context(), path.Root(tc.field), &got))
			if got.ValueString() != tc.want {
				t.Fatal("wrong imported identity")
			}
			if tc.name == "api_key" {
				var secret types.String
				noErrors(t, resp.State.GetAttribute(t.Context(), path.Root("secret"), &secret))
				if !secret.IsNull() || resp.Diagnostics.WarningsCount() != 1 {
					t.Fatal("import recovered a secret or omitted warning")
				}
			}
			for _, bad := range []string{"", tc.id + "/extra", strings.Replace(tc.id, projectID, "project_"+projectID, 1), strings.ToLower(tc.id)} {
				failed := resource.ImportStateResponse{State: emptyState(t, tc.r)}
				tc.r.(resource.ResourceWithImportState).ImportState(t.Context(), resource.ImportStateRequest{ID: bad}, &failed)
				if !failed.Diagnostics.HasError() {
					t.Fatalf("accepted malformed import %q", bad)
				}
			}
		})
	}
}

func TestCreatedKeyWithoutSecretRetainsRevocableIdentity(t *testing.T) {
	r := &apiKeyResource{clients: fakeClients(t, step{method: "POST", path: projectPath + "/api-keys", response: `{"api_key":` + keyJSON() + `}`, status: 201})}
	m := keyState(t)
	m.ID = types.StringUnknown()
	m.Secret = types.StringUnknown()
	resp := resource.CreateResponse{State: tfsdk.State{Schema: schemaFor(t, r)}}
	r.Create(t.Context(), resource.CreateRequest{Plan: planFor(t, r, m)}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("missing one-time secret was not diagnosed")
	}
	noErrors(t, resp.State.Get(t.Context(), &m))
	if m.ID.ValueString() != keyID || !m.Secret.IsNull() {
		t.Fatal("lost identity or saved an empty secret")
	}
}

func TestDatabaseInvalidCreationPlacementFailsBeforeMutation(t *testing.T) {
	for _, all := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty listed", true: "all with replicas"}[all], func(t *testing.T) {
			r := &databaseResource{clients: fakeClients(t)}
			m := databaseState()
			m.ID = types.StringUnknown()
			m.AllRegions = types.BoolValue(all)
			if all {
				m.ReplicaRegions, _ = types.SetValueFrom(t.Context(), types.StringType, []string{"fra-1"})
			}
			resp := resource.CreateResponse{State: emptyState(t, r)}
			r.Create(t.Context(), resource.CreateRequest{Plan: planFor(t, r, m)}, &resp)
			if !resp.Diagnostics.HasError() || !resp.State.Raw.IsNull() {
				t.Fatal("invalid placement creation must fail without creating a database")
			}
		})
	}
}

func TestDatabaseAllRegionsCreationAllowsEmptyReplicaSet(t *testing.T) {
	for _, omitted := range []bool{false, true} {
		t.Run(map[bool]string{false: "explicit empty", true: "omitted"}[omitted], func(t *testing.T) {
			r := &databaseResource{clients: fakeClients(t, step{method: "POST", path: databasesPath, body: `{"name":"main","homeRegion":"gru-1","replicaRegions":null,"allRegions":true}`, response: databaseJSON(true, `[]`), status: 201})}
			m := databaseState()
			m.ID = types.StringUnknown()
			m.AllRegions = types.BoolValue(true)
			if omitted {
				m.ReplicaRegions = types.SetUnknown(types.StringType)
			}
			resp := resource.CreateResponse{State: emptyState(t, r)}
			r.Create(t.Context(), resource.CreateRequest{Plan: planFor(t, r, m)}, &resp)
			noErrors(t, resp.Diagnostics)
			noErrors(t, resp.State.Get(t.Context(), &m))
			if !m.AllRegions.ValueBool() || m.ReplicaRegions.IsNull() || m.ReplicaRegions.IsUnknown() || len(m.ReplicaRegions.Elements()) != 0 {
				t.Fatal("all-region placement must retain a known empty replica set in state")
			}
		})
	}
}

func TestDatabaseDoesNotAdoptOrMutateRecreatedName(t *testing.T) {
	for _, operation := range []string{"read", "update", "delete"} {
		t.Run(operation, func(t *testing.T) {
			r := &databaseResource{clients: fakeClients(t, step{method: "GET", path: databasePath, response: strings.Replace(databaseJSON(false, `[]`), databaseID, keyID, 1)})}
			state := stateFor(t, r, databaseState())
			switch operation {
			case "read":
				resp := resource.ReadResponse{State: state}
				r.Read(t.Context(), resource.ReadRequest{State: state}, &resp)
				noErrors(t, resp.Diagnostics)
				if !resp.State.Raw.IsNull() {
					t.Fatal("adopted recreated database")
				}
			case "update":
				resp := resource.UpdateResponse{State: state}
				r.Update(t.Context(), resource.UpdateRequest{State: state, Plan: planFor(t, r, databaseState())}, &resp)
				if !resp.Diagnostics.HasError() || !resp.State.Raw.Equal(state.Raw) {
					t.Fatal("changed recreated database")
				}
			case "delete":
				var resp resource.DeleteResponse
				r.Delete(t.Context(), resource.DeleteRequest{State: state}, &resp)
				if !resp.Diagnostics.HasError() {
					t.Fatal("deleted recreated database")
				}
			}
		})
	}
}

func TestExpiredUserTokenPreservesStateAndRedactsDiagnostics(t *testing.T) {
	for _, kind := range []string{"project", "database"} {
		t.Run(kind, func(t *testing.T) {
			var r resource.Resource
			var model any
			if kind == "project" {
				r = &projectResource{clients: fakeClients(t, step{method: "GET", path: projectPath, status: 401, response: `{"error":{"code":"invalid_token","message":"fixture.user.signature"}}`})}
				model = projectState()
			} else {
				r = &databaseResource{clients: fakeClients(t, step{method: "POST", path: "/terrascale/oauth/token", status: 400, response: `{"error":"invalid_grant","error_description":"fixture.user.signature"}`})}
				model = databaseState()
			}
			state := stateFor(t, r, model)
			resp := resource.ReadResponse{State: state}
			r.Read(t.Context(), resource.ReadRequest{State: state}, &resp)
			if !resp.Diagnostics.HasError() || !resp.State.Raw.Equal(state.Raw) {
				t.Fatal("expired token did not preserve state with error")
			}
			for _, d := range resp.Diagnostics {
				if strings.Contains(d.Detail(), "fixture") || !strings.Contains(d.Detail(), "current ts-auth user access token") {
					t.Fatal("expired-token diagnostic is unsafe or not actionable")
				}
			}
		})
	}
}

func TestDatabaseImportLookupPagination(t *testing.T) {
	for _, mode := range []string{"found", "absent", "repeated cursor"} {
		t.Run(mode, func(t *testing.T) {
			steps := []step{{method: "GET", path: databasesPath + "?limit=100", response: `{"items":[],"next":"` + projectID + `"}`}}
			response := `{"items":[],"next":""}`
			if mode == "found" {
				response = `{"items":[` + databaseJSON(false, `[]`) + `],"next":""}`
			}
			if mode == "repeated cursor" {
				response = steps[0].response
			}
			steps = append(steps, step{method: "GET", path: databasesPath + "?after=" + projectID + "&limit=100", response: response})
			if mode == "found" {
				steps = append(steps, step{method: "GET", path: databasePath, response: databaseJSON(false, `[]`)})
			}
			r := &databaseResource{clients: fakeClients(t, steps...)}
			m := databaseState()
			m.Name = types.StringNull()
			state := stateFor(t, r, m)
			resp := resource.ReadResponse{State: state}
			r.Read(t.Context(), resource.ReadRequest{State: state}, &resp)
			if mode == "repeated cursor" {
				if !resp.Diagnostics.HasError() || !resp.State.Raw.Equal(state.Raw) {
					t.Fatal("broken lookup did not preserve state with error")
				}
			} else {
				noErrors(t, resp.Diagnostics)
				if resp.State.Raw.IsNull() != (mode == "absent") {
					t.Fatal("incorrect imported identity lookup")
				}
				if mode == "found" {
					noErrors(t, resp.State.Get(t.Context(), &m))
					if m.Name.ValueString() != "main" || m.ID.ValueString() != databaseID {
						t.Fatal("imported identity was not resolved")
					}
				}
			}
		})
	}
}

func TestDatabaseDeleteNeverClearsReplicas(t *testing.T) {
	for _, all := range []bool{false, true} {
		t.Run(map[bool]string{false: "listed", true: "all regions"}[all], func(t *testing.T) {
			regions := `["fra-1"]`
			if all {
				regions = `[]`
			}
			r := &databaseResource{clients: fakeClients(t,
				step{method: "GET", path: databasePath, response: databaseJSON(all, regions)},
				step{method: "DELETE", path: databasePath, status: 409, response: `{"error":{"code":"database_replicated","message":"fixture-secret"}}`},
			)}
			m := databaseState()
			m.AllRegions = types.BoolValue(all)
			if !all {
				m.ReplicaRegions, _ = types.SetValueFrom(t.Context(), types.StringType, []string{"fra-1"})
			}
			var resp resource.DeleteResponse
			r.Delete(t.Context(), resource.DeleteRequest{State: stateFor(t, r, m)}, &resp)
			if !resp.Diagnostics.HasError() {
				t.Fatal("replicated deletion failure ignored")
			}
			for _, d := range resp.Diagnostics {
				if strings.Contains(d.Detail(), "fixture-secret") {
					t.Fatal("server message leaked")
				}
			}
		})
	}
}
