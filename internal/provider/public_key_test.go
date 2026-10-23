package provider

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type publicTestTransport func(*http.Request) (*http.Response, error)

func (f publicTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestConfiguredProjectCredentialCollectionLifecycle(t *testing.T) {
	for _, credential := range []string{"public key", "project JWT", "rejected key"} {
		t.Run(credential, func(t *testing.T) {
			t.Setenv("TERRASCALE_ACCESS_TOKEN", "")
			t.Setenv("TERRASCALE_PROJECT_TOKEN", "")
			key := keyID + "_" + strings.Repeat("a", 52)
			var keyCreations, exchanges, collectionRequests atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch req.URL.Path {
				case projectPath + "/api-keys":
					keyCreations.Add(1)
					if req.Method != "POST" || req.Host != "auth.terrascale.tech" || req.Header.Get("Authorization") != "Bearer fixture.user.signature" {
						t.Error("incorrect public key creation request")
					}
					var body map[string]any
					if err := json.NewDecoder(req.Body).Decode(&body); err != nil || !reflect.DeepEqual(body, map[string]any{"name": "reader", "database": "main", "scopes": []any{"documents:read", "documents:write"}}) {
						t.Error("incorrect project key permissions or restriction")
					}
					w.WriteHeader(http.StatusCreated)
					metadata := strings.Replace(keyJSON(), `["documents:read"]`, `["documents:read","documents:write"]`, 1)
					_ = json.NewEncoder(w).Encode(map[string]any{"api_key": json.RawMessage(metadata), "key": key})
				case "/terrascale/oauth/token":
					exchanges.Add(1)
					client, secret, ok := req.BasicAuth()
					if req.Method != "POST" || req.Host != "auth.terrascale.tech" || !ok || client != keyID || secret != key || keyCreations.Load() != 1 {
						t.Error("incorrect public key client authentication")
					}
					if err := req.ParseForm(); err != nil || !reflect.DeepEqual(req.Form, url.Values{"grant_type": {"client_credentials"}, "scope": {"documents:read documents:write"}}) {
						t.Error("key exchange must use collection scopes and the key's stored project, without a project override")
					}
					if credential == "rejected key" {
						w.WriteHeader(http.StatusUnauthorized)
						_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_client", "error_description": key})
						return
					}
					_, _ = io.WriteString(w, `{"access_token":"fixture.project.signature","token_type":"Bearer","expires_in":3600}`)
				case strings.TrimSuffix(collectionPath, "/users"), collectionPath:
					collectionRequests.Add(1)
					if req.Host != "api.terrascale.tech" || req.Header.Get("Authorization") != "Bearer fixture.project.signature" {
						t.Error("collection request did not use a project access token")
					}
					if req.Method == "POST" {
						w.WriteHeader(http.StatusCreated)
					} else if req.Method != "GET" && req.Method != "DELETE" {
						t.Error("unexpected collection operation")
					}
					_, _ = io.WriteString(w, collectionJSON(collectionID))
				default:
					t.Errorf("unexpected public route %s", req.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			t.Cleanup(server.Close)
			base, err := url.Parse(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			// Exercise real Configure with fixed public origins; route all traffic to the local fake.
			hc := &http.Client{Transport: publicTestTransport(func(req *http.Request) (*http.Response, error) {
				local := req.Clone(req.Context())
				local.Host = req.URL.Host
				local.URL.Scheme, local.URL.Host = base.Scheme, base.Host
				return http.DefaultTransport.RoundTrip(local)
			})}
			configureProvider := func(model providerModel) *clients {
				p := &terraScaleProvider{version: "test", httpClient: hc}
				var s provider.SchemaResponse
				p.Schema(t.Context(), provider.SchemaRequest{}, &s)
				state := tfsdk.State{Schema: s.Schema}
				noErrors(t, state.Set(t.Context(), model))
				var resp provider.ConfigureResponse
				p.Configure(t.Context(), provider.ConfigureRequest{Config: tfsdk.Config{Schema: s.Schema, Raw: state.Raw}}, &resp)
				noErrors(t, resp.Diagnostics)
				return resp.ResourceData.(*clients)
			}
			management := configureProvider(providerModel{AccessToken: types.StringValue("fixture.user.signature"), ProjectToken: types.StringNull()})
			keyResource := &apiKeyResource{clients: management}
			keyModel := keyState(t)
			keyModel.ID, keyModel.Secret = types.StringUnknown(), types.StringUnknown()
			keyModel.Scopes, _ = types.SetValueFrom(t.Context(), types.StringType, []string{"documents:read", "documents:write"})
			createdKey := resource.CreateResponse{State: emptyState(t, keyResource)}
			keyResource.Create(t.Context(), resource.CreateRequest{Plan: planFor(t, keyResource, keyModel)}, &createdKey)
			noErrors(t, createdKey.Diagnostics)
			noErrors(t, createdKey.State.Get(t.Context(), &keyModel))
			projectToken := keyModel.Secret
			if credential == "project JWT" {
				projectToken = types.StringValue("fixture.project.signature")
			}
			alias := configureProvider(providerModel{AccessToken: types.StringNull(), ProjectToken: projectToken})
			col := &collectionResource{clients: alias}
			model := collectionState()
			model.ID = types.StringUnknown()
			created := resource.CreateResponse{State: emptyState(t, col)}
			col.Create(t.Context(), resource.CreateRequest{Plan: planFor(t, col, model)}, &created)
			if credential == "rejected key" {
				if !created.Diagnostics.HasError() || !created.State.Raw.IsNull() || collectionRequests.Load() != 0 || exchanges.Load() != 1 {
					t.Fatal("rejected exchange sent a collection mutation or failed to diagnose")
				}
				for _, d := range created.Diagnostics {
					if strings.Contains(d.Detail(), key) || strings.Contains(d.Summary(), key) {
						t.Fatal("key leaked in diagnostics")
					}
				}
				return
			}
			noErrors(t, created.Diagnostics)
			read := resource.ReadResponse{State: created.State}
			col.Read(t.Context(), resource.ReadRequest{State: created.State}, &read)
			noErrors(t, read.Diagnostics)
			noErrors(t, read.State.Get(t.Context(), &model))
			if model.ID.ValueString() != collectionID {
				t.Fatal("collection identity was not retained")
			}
			var deleted resource.DeleteResponse
			col.Delete(t.Context(), resource.DeleteRequest{State: read.State}, &deleted)
			noErrors(t, deleted.Diagnostics)
			wantExchanges := int64(1)
			if credential == "project JWT" {
				wantExchanges = 0
			}
			if exchanges.Load() != wantExchanges || collectionRequests.Load() != 4 {
				t.Fatal("incorrect exchange caching or collection lifecycle request count")
			}
		})
	}
}
