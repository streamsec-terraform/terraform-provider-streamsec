package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// These tests go through the provider server, as Terraform does: the schema
// Terraform sees, config validation, RequiresReplace and import.

func protocolServer(t *testing.T) (tfprotov6.ProviderServer, *tfprotov6.GetProviderSchemaResponse) {
	t.Helper()
	server := providerserver.NewProtocol6(New("test")())()
	resp, err := server.GetProviderSchema(context.Background(), &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatalf("GetProviderSchema: %s", err)
	}
	requireNoProtocolErrors(t, resp.Diagnostics)
	return server, resp
}

func requireNoProtocolErrors(t *testing.T, diags []*tfprotov6.Diagnostic) {
	t.Helper()
	for _, d := range diags {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			t.Fatalf("error: %s: %s", d.Summary, d.Detail)
		}
	}
}

func protocolErrors(diags []*tfprotov6.Diagnostic) string {
	var parts []string
	for _, d := range diags {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			parts = append(parts, d.Summary+": "+d.Detail)
		}
	}
	return strings.Join(parts, "; ")
}

type wantAttribute struct {
	typ                                     tftypes.Type
	required, optional, computed, sensitive bool
}

var stringSetType = tftypes.Set{ElementType: tftypes.String}

func TestScannerHubSchemasAsTerraformSeesThem(t *testing.T) {
	_, resp := protocolServer(t)

	cases := []struct {
		name   string
		schema *tfprotov6.Schema
		attrs  map[string]wantAttribute
	}{
		{"data source streamsec_aws_scanner_hub_access", resp.DataSourceSchemas["streamsec_aws_scanner_hub_access"], map[string]wantAttribute{
			"hub_account_id":             {typ: tftypes.String, required: true},
			"hub_cloud_account_id":       {typ: tftypes.String, computed: true},
			"role_name":                  {typ: tftypes.String, computed: true},
			"external_id":                {typ: tftypes.String, computed: true, sensitive: true},
			"trusted_role_name_patterns": {typ: tftypes.List{ElementType: tftypes.String}, computed: true},
		}},
		{"resource streamsec_aws_scanner_hub", resp.ResourceSchemas["streamsec_aws_scanner_hub"], map[string]wantAttribute{
			"account_id":  {typ: tftypes.String, required: true},
			"coverage":    {typ: tftypes.String, required: true},
			"account_ids": {typ: stringSetType, optional: true},
		}},
		{"resource streamsec_aws_volume_scanner_ack", resp.ResourceSchemas["streamsec_aws_volume_scanner_ack"], map[string]wantAttribute{
			"account_id":     {typ: tftypes.String, required: true},
			"region":         {typ: tftypes.String, required: true},
			"module_version": {typ: tftypes.String, required: true},
		}},
		{"resource streamsec_aws_scanner_access_ack", resp.ResourceSchemas["streamsec_aws_scanner_access_ack"], map[string]wantAttribute{
			"hub_account_id":         {typ: tftypes.String, required: true},
			"spoke_cloud_account_id": {typ: tftypes.String, required: true},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.schema == nil {
				t.Fatal("not registered with the provider")
			}
			got := map[string]*tfprotov6.SchemaAttribute{}
			for _, a := range tc.schema.Block.Attributes {
				got[a.Name] = a
			}
			if len(got) != len(tc.attrs) {
				names := make([]string, 0, len(got))
				for name := range got {
					names = append(names, name)
				}
				t.Errorf("attributes = %v, want exactly %d", names, len(tc.attrs))
			}
			for name, want := range tc.attrs {
				a, ok := got[name]
				if !ok {
					t.Errorf("missing attribute %s", name)
					continue
				}
				if !a.Type.Equal(want.typ) || a.Required != want.required || a.Optional != want.optional ||
					a.Computed != want.computed || a.Sensitive != want.sensitive {
					t.Errorf("%s = {type %s required %v optional %v computed %v sensitive %v}, want %+v",
						name, a.Type, a.Required, a.Optional, a.Computed, a.Sensitive, want)
				}
			}
		})
	}
}

func objectValue(t *testing.T, schema *tfprotov6.Schema, attrs map[string]tftypes.Value) *tfprotov6.DynamicValue {
	t.Helper()
	typ := schema.ValueType()
	dv, err := tfprotov6.NewDynamicValue(typ, tftypes.NewValue(typ, attrs))
	if err != nil {
		t.Fatalf("NewDynamicValue: %s", err)
	}
	return &dv
}

func str(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }

func strSet(values ...string) tftypes.Value {
	elements := make([]tftypes.Value, len(values))
	for i, v := range values {
		elements[i] = str(v)
	}
	return tftypes.NewValue(stringSetType, elements)
}

var nullStrSet = tftypes.NewValue(stringSetType, nil)

func TestScannerHubConfigValidation(t *testing.T) {
	server, resp := protocolServer(t)

	cases := []struct {
		name      string
		typeName  string
		attrs     map[string]tftypes.Value
		wantError string
	}{
		{"selected with accounts", "streamsec_aws_scanner_hub",
			map[string]tftypes.Value{"account_id": str("hub-1"), "coverage": str("selected"), "account_ids": strSet("acc-a")}, ""},
		{"all", "streamsec_aws_scanner_hub",
			map[string]tftypes.Value{"account_id": str("hub-1"), "coverage": str("all"), "account_ids": nullStrSet}, ""},
		{"coverage none is for destroy only", "streamsec_aws_scanner_hub",
			map[string]tftypes.Value{"account_id": str("hub-1"), "coverage": str("none"), "account_ids": nullStrSet}, "coverage"},
		{"selected needs account_ids", "streamsec_aws_scanner_hub",
			map[string]tftypes.Value{"account_id": str("hub-1"), "coverage": str("selected"), "account_ids": nullStrSet}, "account_ids"},
		{"all takes no account_ids", "streamsec_aws_scanner_hub",
			map[string]tftypes.Value{"account_id": str("hub-1"), "coverage": str("all"), "account_ids": strSet("acc-a")}, "account_ids"},
		{"empty account id in account_ids", "streamsec_aws_scanner_hub",
			map[string]tftypes.Value{"account_id": str("hub-1"), "coverage": str("selected"), "account_ids": strSet("")}, "account_ids"},
		{"spoke id is 12 digits", "streamsec_aws_scanner_access_ack",
			map[string]tftypes.Value{"hub_account_id": str("hub-1"), "spoke_cloud_account_id": str("4444")}, "spoke_cloud_account_id"},
		{"spoke ok", "streamsec_aws_scanner_access_ack",
			map[string]tftypes.Value{"hub_account_id": str("hub-1"), "spoke_cloud_account_id": str("444455556666")}, ""},
		{"region shape", "streamsec_aws_volume_scanner_ack",
			map[string]tftypes.Value{"account_id": str("acc-1"), "region": str("US East 1"), "module_version": str("1.16.0")}, "region"},
		// The backend's format: v?MAJOR.MINOR.PATCH(-pre)?(+build)?, at most 64 characters.
		{"module_version too long", "streamsec_aws_volume_scanner_ack",
			map[string]tftypes.Value{"account_id": str("acc-1"), "region": str("us-east-1"), "module_version": str("1.2.3-" + strings.Repeat("a", 59))}, "module_version"},
		{"module_version of 64", "streamsec_aws_volume_scanner_ack",
			map[string]tftypes.Value{"account_id": str("acc-1"), "region": str("us-east-1"), "module_version": str("1.2.3-" + strings.Repeat("a", 58))}, ""},
		{"module_version with v, pre-release and build", "streamsec_aws_volume_scanner_ack",
			map[string]tftypes.Value{"account_id": str("acc-1"), "region": str("us-east-1"), "module_version": str("v1.16.0-rc.1+build.5")}, ""},
		{"module_version not a version", "streamsec_aws_volume_scanner_ack",
			map[string]tftypes.Value{"account_id": str("acc-1"), "region": str("us-east-1"), "module_version": str("latest")}, "module_version"},
		{"module_version of two parts", "streamsec_aws_volume_scanner_ack",
			map[string]tftypes.Value{"account_id": str("acc-1"), "region": str("us-east-1"), "module_version": str("1.16")}, "module_version"},
		// The whole value must match, as the backend's fullmatch does.
		{"module_version with a prefix", "streamsec_aws_volume_scanner_ack",
			map[string]tftypes.Value{"account_id": str("acc-1"), "region": str("us-east-1"), "module_version": str("release-1.16.0")}, "module_version"},
		{"module_version with trailing junk", "streamsec_aws_volume_scanner_ack",
			map[string]tftypes.Value{"account_id": str("acc-1"), "region": str("us-east-1"), "module_version": str("1.16.0_rc")}, "module_version"},
		{"module_version with an empty suffix", "streamsec_aws_volume_scanner_ack",
			map[string]tftypes.Value{"account_id": str("acc-1"), "region": str("us-east-1"), "module_version": str("1.16.0-")}, "module_version"},
		{"volume ack ok", "streamsec_aws_volume_scanner_ack",
			map[string]tftypes.Value{"account_id": str("acc-1"), "region": str("us-gov-west-1"), "module_version": str("1.16.0")}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := server.ValidateResourceConfig(context.Background(), &tfprotov6.ValidateResourceConfigRequest{
				TypeName: tc.typeName,
				Config:   objectValue(t, resp.ResourceSchemas[tc.typeName], tc.attrs),
			})
			if err != nil {
				t.Fatalf("ValidateResourceConfig: %s", err)
			}
			errs := protocolErrors(out.Diagnostics)
			if tc.wantError == "" && errs != "" {
				t.Fatalf("unexpected errors: %s", errs)
			}
			if tc.wantError != "" {
				var onAttr bool
				for _, d := range out.Diagnostics {
					if d.Severity == tfprotov6.DiagnosticSeverityError && d.Attribute != nil && len(d.Attribute.Steps()) > 0 &&
						d.Attribute.Steps()[0] == tftypes.AttributeName(tc.wantError) {
						onAttr = true
					}
				}
				if !onAttr {
					t.Fatalf("want an error on %s, got: %q", tc.wantError, errs)
				}
			}
		})
	}
}

func TestScannerHubPlanReplacesOnIdentityChanges(t *testing.T) {
	server, resp := protocolServer(t)

	hub := func(accountID, coverage string, ids tftypes.Value) map[string]tftypes.Value {
		return map[string]tftypes.Value{"account_id": str(accountID), "coverage": str(coverage), "account_ids": ids}
	}
	volumeAck := func(accountID, region, version string) map[string]tftypes.Value {
		return map[string]tftypes.Value{"account_id": str(accountID), "region": str(region), "module_version": str(version)}
	}
	accessAck := func(hubID, spoke string) map[string]tftypes.Value {
		return map[string]tftypes.Value{"hub_account_id": str(hubID), "spoke_cloud_account_id": str(spoke)}
	}

	cases := []struct {
		name        string
		typeName    string
		prior, next map[string]tftypes.Value
		wantReplace string // "" = an update in place
	}{
		{"hub account", "streamsec_aws_scanner_hub", hub("hub-1", "all", nullStrSet), hub("hub-2", "all", nullStrSet), "account_id"},
		{"hub coverage", "streamsec_aws_scanner_hub", hub("hub-1", "all", nullStrSet), hub("hub-1", "selected", strSet("acc-a")), ""},
		{"hub accounts", "streamsec_aws_scanner_hub", hub("hub-1", "selected", strSet("acc-a")), hub("hub-1", "selected", strSet("acc-b")), ""},
		{"volume ack account", "streamsec_aws_volume_scanner_ack", volumeAck("acc-1", "us-east-1", "1.16.0"), volumeAck("acc-2", "us-east-1", "1.16.0"), "account_id"},
		{"volume ack region", "streamsec_aws_volume_scanner_ack", volumeAck("acc-1", "us-east-1", "1.16.0"), volumeAck("acc-1", "eu-west-1", "1.16.0"), "region"},
		{"volume ack version", "streamsec_aws_volume_scanner_ack", volumeAck("acc-1", "us-east-1", "1.16.0"), volumeAck("acc-1", "us-east-1", "1.17.0"), ""},
		{"access ack hub", "streamsec_aws_scanner_access_ack", accessAck("hub-1", "444455556666"), accessAck("hub-2", "444455556666"), "hub_account_id"},
		{"access ack spoke", "streamsec_aws_scanner_access_ack", accessAck("hub-1", "444455556666"), accessAck("hub-1", "777788889999"), "spoke_cloud_account_id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			schema := resp.ResourceSchemas[tc.typeName]
			next := objectValue(t, schema, tc.next)
			out, err := server.PlanResourceChange(context.Background(), &tfprotov6.PlanResourceChangeRequest{
				TypeName:         tc.typeName,
				PriorState:       objectValue(t, schema, tc.prior),
				ProposedNewState: next,
				Config:           next,
			})
			if err != nil {
				t.Fatalf("PlanResourceChange: %s", err)
			}
			requireNoProtocolErrors(t, out.Diagnostics)
			var replace []string
			for _, p := range out.RequiresReplace {
				replace = append(replace, p.String())
			}
			if tc.wantReplace == "" && len(replace) > 0 {
				t.Errorf("RequiresReplace = %v, want an update in place", replace)
			}
			if tc.wantReplace != "" && (len(out.RequiresReplace) != 1 ||
				!out.RequiresReplace[0].Equal(tftypes.NewAttributePath().WithAttributeName(tc.wantReplace))) {
				t.Errorf("RequiresReplace = %v, want [%s]", replace, tc.wantReplace)
			}
		})
	}
}

// volume-scanner-access depends on its hub, so while the hub has changes Terraform reads the
// module's data sources at apply and spoke_cloud_account_id is unknown at plan. That alone
// must not replace the ack, which would record the spoke's access removed, then deployed.
func TestScannerAccessAckPlanKeepsSpokeWhileUnknown(t *testing.T) {
	server, resp := protocolServer(t)
	schema := resp.ResourceSchemas["streamsec_aws_scanner_access_ack"]
	unknown := tftypes.NewValue(tftypes.String, tftypes.UnknownValue)
	config := objectValue(t, schema, map[string]tftypes.Value{"hub_account_id": str("hub-1"), "spoke_cloud_account_id": unknown})

	cases := []struct {
		name      string
		prior     tftypes.Value
		wantSpoke tftypes.Value
	}{
		{"existing ack keeps its spoke", tftypes.NewValue(schema.ValueType(), map[string]tftypes.Value{
			"hub_account_id": str("hub-1"), "spoke_cloud_account_id": str("444455556666"),
		}), str("444455556666")},
		// Also how Terraform plans the new object of a replacement.
		{"new ack waits for apply", tftypes.NewValue(schema.ValueType(), nil), unknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prior, err := tfprotov6.NewDynamicValue(schema.ValueType(), tc.prior)
			if err != nil {
				t.Fatalf("NewDynamicValue: %s", err)
			}
			out, err := server.PlanResourceChange(context.Background(), &tfprotov6.PlanResourceChangeRequest{
				TypeName:         "streamsec_aws_scanner_access_ack",
				PriorState:       &prior,
				ProposedNewState: config,
				Config:           config,
			})
			if err != nil {
				t.Fatalf("PlanResourceChange: %s", err)
			}
			requireNoProtocolErrors(t, out.Diagnostics)
			if len(out.RequiresReplace) > 0 {
				t.Errorf("RequiresReplace = %v, want none", out.RequiresReplace)
			}
			planned, err := out.PlannedState.Unmarshal(schema.ValueType())
			if err != nil {
				t.Fatalf("Unmarshal: %s", err)
			}
			var attrs map[string]tftypes.Value
			if err := planned.As(&attrs); err != nil {
				t.Fatalf("As: %s", err)
			}
			if !attrs["spoke_cloud_account_id"].Equal(tc.wantSpoke) {
				t.Errorf("planned spoke_cloud_account_id = %s, want %s", attrs["spoke_cloud_account_id"], tc.wantSpoke)
			}
		})
	}
}

func TestScannerHubImportThroughTheProvider(t *testing.T) {
	server, resp := protocolServer(t)

	out, err := server.ImportResourceState(context.Background(), &tfprotov6.ImportResourceStateRequest{
		TypeName: "streamsec_aws_scanner_hub",
		ID:       "hub-1",
	})
	if err != nil {
		t.Fatalf("ImportResourceState: %s", err)
	}
	requireNoProtocolErrors(t, out.Diagnostics)
	if len(out.ImportedResources) != 1 {
		t.Fatalf("imported %d resources, want 1", len(out.ImportedResources))
	}
	value, err := out.ImportedResources[0].State.Unmarshal(resp.ResourceSchemas["streamsec_aws_scanner_hub"].ValueType())
	if err != nil {
		t.Fatalf("Unmarshal: %s", err)
	}
	var attrs map[string]tftypes.Value
	if err := value.As(&attrs); err != nil {
		t.Fatalf("As: %s", err)
	}
	var accountID string
	if err := attrs["account_id"].As(&accountID); err != nil || accountID != "hub-1" {
		t.Errorf("imported account_id = %q (%v), want hub-1", accountID, err)
	}
}
