package provider

import (
	"net/http"
	"testing"

	"terraform-provider-streamsec/internal/client"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// The calls whose body moving off GraphQL had to rebuild: each sends what the
// GraphQL operation sent.

func TestAzureTenantCreateSendsTheTenantAsCloudAccountID(t *testing.T) {
	api, c := newFakeAPI(t, map[string]fakeResponse{
		"POST " + client.CloudAccountsPath: {200, `{"_id":"acc-1","account_token":"tok-1"}`},
	})
	r := NewAzureTenantResource()
	s := configured(t, r, c)

	state, diags := doCreate(t, r, s, AzureTenantResourceModel{
		ID:             types.StringUnknown(),
		CloudAccountID: types.StringValue("tenant-1"),
		DisplayName:    types.StringValue("azure"),
		AccountToken:   types.StringUnknown(),
	})
	requireNoDiags(t, diags)

	api.expectCalls(apiCall{Method: http.MethodPost, URI: client.CloudAccountsPath, Body: map[string]interface{}{
		"account_type": "Azure", "cloud_account_id": "tenant-1", "display_name": "azure",
	}})
	var got AzureTenantResourceModel
	getState(t, state, &got)
	if got.ID.ValueString() != "acc-1" || got.AccountToken.ValueString() != "tok-1" {
		t.Errorf("state = %+v", got)
	}
}

// The GraphQL create never passed client_email and private_key on.
func TestGoogleWorkspaceCreateSendsOnlyTheAccountFields(t *testing.T) {
	api, c := newFakeAPI(t, map[string]fakeResponse{
		"POST " + client.CloudAccountsPath: {200, `{"_id":"acc-2","account_token":"tok-2"}`},
	})
	r := NewGoogleWorkspaceResource()
	s := configured(t, r, c)

	_, diags := doCreate(t, r, s, GoogleWorkspaceResourceModel{
		ID:             types.StringUnknown(),
		DisplayName:    types.StringValue("workspace"),
		CloudAccountID: types.StringValue("C0123"),
		ClientEmail:    types.StringValue("sa@example.com"),
		PrivateKey:     types.StringValue("key"),
		AccountToken:   types.StringUnknown(),
	})
	requireNoDiags(t, diags)

	api.expectCalls(apiCall{Method: http.MethodPost, URI: client.CloudAccountsPath, Body: map[string]interface{}{
		"account_type": "GOOGLE_WORKSPACE", "cloud_account_id": "C0123", "display_name": "workspace",
	}})
}

func TestKubernetesClusterCreateSendsTheARNAsEKSARN(t *testing.T) {
	api, c := newFakeAPI(t, map[string]fakeResponse{
		"POST " + kubernetesPath: {200, `{"_id":"cl-1","status":"not-deployed","collection_token":"ct","creation_date":"2026-01-02"}`},
	})
	r := NewAWSKubernetesClusterResource()
	s := configured(t, r, c)

	state, diags := doCreate(t, r, s, AWSKubernetesClusterResourceModel{
		ID:              types.StringUnknown(),
		ARN:             types.StringValue("arn:aws:eks:us-east-1:000000000000:cluster/c"),
		DisplayName:     types.StringValue("c"),
		Status:          types.StringUnknown(),
		CollectionToken: types.StringUnknown(),
		CreationDate:    types.StringUnknown(),
	})
	requireNoDiags(t, diags)

	api.expectCalls(apiCall{Method: http.MethodPost, URI: kubernetesPath, Body: map[string]interface{}{
		"display_name": "c", "eks_arn": "arn:aws:eks:us-east-1:000000000000:cluster/c",
	}})
	var got AWSKubernetesClusterResourceModel
	getState(t, state, &got)
	if got.ID.ValueString() != "cl-1" || got.CollectionToken.ValueString() != "ct" || got.CreationDate.ValueString() != "2026-01-02" {
		t.Errorf("state = %+v", got)
	}
}

func TestKubernetesClusterUpdateAndDeleteUseTheClusterPath(t *testing.T) {
	api, c := newFakeAPI(t, map[string]fakeResponse{
		"PUT " + kubernetesPath + "/cl-1":    {200, `{"_id":"cl-1"}`},
		"DELETE " + kubernetesPath + "/cl-1": {200, `true`},
	})
	r := NewAWSKubernetesClusterResource()
	s := configured(t, r, c)
	prior := AWSKubernetesClusterResourceModel{
		ID:              types.StringValue("cl-1"),
		ARN:             types.StringValue("arn"),
		DisplayName:     types.StringValue("old"),
		Status:          types.StringValue("connected"),
		CollectionToken: types.StringValue("ct"),
		CreationDate:    types.StringValue("2026-01-02"),
	}
	plan := prior
	plan.DisplayName = types.StringValue("new")

	_, diags := doUpdate(t, r, s, prior, plan)
	requireNoDiags(t, diags)
	requireNoDiags(t, doDelete(t, r, s, plan))

	api.expectCalls(
		apiCall{Method: http.MethodPut, URI: kubernetesPath + "/cl-1", Body: map[string]interface{}{"display_name": "new"}},
		apiCall{Method: http.MethodDelete, URI: kubernetesPath + "/cl-1"},
	)
}

// The acknowledgement goes with the account's own token, not the provider's, and
// no workspace.
func TestAWSAccountAckSendsTheAccountToken(t *testing.T) {
	api, c := newFakeAPI(t, map[string]fakeResponse{
		"GET " + client.CloudAccountsPath:                     {200, `[{"_id":"acc-1","cloud_account_id":"000000000000","account_auth_token":"account-tok"}]`},
		"POST " + client.AccountsPath("acc-1", "acknowledge"): {200, `{}`},
	})
	r := NewAWSAccountAckResource()
	s := configured(t, r, c)

	_, diags := doCreate(t, r, s, AWSAccountAckResourceModel{
		ID:             types.StringUnknown(),
		RoleARN:        types.StringValue("arn:aws:iam::000000000000:role/r"),
		CloudAccountID: types.StringValue("000000000000"),
		StackRegion:    types.StringValue("us-east-1"),
	})
	requireNoDiags(t, diags)

	calls := api.recorded()
	if len(calls) != 2 {
		t.Fatalf("calls = %v", calls)
	}
	ack := calls[1]
	if ack.Method != http.MethodPost || ack.URI != "/api/accounts/accounts/acc-1/acknowledge" {
		t.Errorf("ack = %s %s", ack.Method, ack.URI)
	}
	if ack.Auth != "Bearer account-tok" || ack.Customer != "" {
		t.Errorf("ack Authorization = %q, customer = %q, want the account's token and no workspace", ack.Auth, ack.Customer)
	}
	if ack.Body["role_arn"] != "arn:aws:iam::000000000000:role/r" || ack.Body["account_type"] != "AWS" || ack.Body["init_stack_version"] != float64(1) {
		t.Errorf("ack body = %v", ack.Body)
	}
}
