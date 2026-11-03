package provider

import (
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/resource"
)

func NewGCPScannerHubResource() resource.Resource {
	return &ScannerHubResource{cloud: gcpScannerHub}
}

// gcpScannerHub makes a GCP project's agentless scanner a hub that also scans other GCP
// projects, as its own service account.
var gcpScannerHub = scannerHubCloud{
	account: accountKind{
		accountType:  "GCP",
		noun:         "a GCP project",
		resourceType: "streamsec_gcp_scanner_hub",
		ref:          "streamsec_gcp_project.<name>.id",
		nativeID:     "the GCP project id",
	},
	description: "Makes a GCP project's agentless volume scanner a hub: it also scans other GCP projects, " +
		"as its own service account, where that account holds the scanned-projects role (the google-integration " +
		"module's `modules/volume-scanner-hub-access`, or the console's command, grants it). " +
		"Destroying it turns the hub off: each project goes back to its own scanner.\n\n" + pinnedAccountDescription,
	accountIDDescription: "The Stream Security id of the hub's GCP project (`streamsec_gcp_project.<name>.id`). Changing it replaces the resource.",
	coverageDescription: "Which projects the hub scans: `all` (every other GCP project without a scanner of its own " +
		"and not scanned by another hub) or `selected` (the projects in `account_ids`). Only one hub may cover `all`.",
	accountIDsDescription: "The Stream Security ids of the GCP projects the hub scans (`streamsec_gcp_project.<name>.id`). " +
		"Required when `coverage` is `selected` (an empty set scans none); must be unset or empty when `coverage` is `all`.",
	hints: statusHints{
		http.StatusBadRequest: "Stream Security refused the coverage: account_id must be a connected GCP project with a scanner, and each of " +
			"account_ids a connected GCP project other than a hub. Both take Stream Security account ids " +
			"(streamsec_gcp_project.<name>.id), not GCP project ids.",
		http.StatusForbidden: scannerHubForbiddenHint,
		http.StatusNotFound:  "account_id is not an account in Stream Security: it takes the Stream Security account id (streamsec_gcp_project.<name>.id).",
		http.StatusConflict: `Another project's scanner already scans all GCP projects (only one hub may cover "all"), or one of ` +
			"account_ids is already scanned by another hub. Change that hub's coverage first. " + pinnedAccountNote,
	},
}
