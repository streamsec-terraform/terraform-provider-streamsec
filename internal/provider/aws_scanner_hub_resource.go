package provider

import (
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/resource"
)

func NewAWSScannerHubResource() resource.Resource {
	return &ScannerHubResource{cloud: awsScannerHub}
}

// awsScannerHub makes an AWS account's agentless scanner a hub that also scans other AWS
// accounts, through the StreamScannerSpokeRole each of them creates.
var awsScannerHub = scannerHubCloud{
	account: accountKind{
		accountType:  "AWS",
		noun:         "an AWS account",
		resourceType: "streamsec_aws_scanner_hub",
		ref:          "streamsec_aws_account.<name>.id",
		nativeID:     "the 12-digit AWS account id",
	},
	description: "Makes an AWS account's agentless volume scanner a hub: it also scans other AWS accounts in its regions, " +
		"through the `StreamScannerSpokeRole` each of them creates (see the `streamsec_aws_scanner_hub_access` data source). " +
		"Destroying it turns the hub off: each account goes back to its own scanner.\n\n" + pinnedAccountDescription,
	accountIDDescription: "The Stream Security id of the hub's AWS account (`streamsec_aws_account.<name>.id`). Changing it replaces the resource.",
	coverageDescription: "Which accounts the hub scans: `all` (every other AWS account without a scanner of its own in the region " +
		"and not scanned by another hub) or `selected` (the accounts in `account_ids`). Only one hub may cover `all`.",
	accountIDsDescription: "The Stream Security ids of the AWS accounts the hub scans. Required when `coverage` is `selected` " +
		"(an empty set scans none); must be unset or empty when `coverage` is `all`.",
	hints: statusHints{
		http.StatusBadRequest: "Stream Security refused the coverage: account_id must be a connected AWS account, and each of " +
			"account_ids a connected AWS account other than a hub. Both take Stream Security account ids " +
			"(streamsec_aws_account.<name>.id), not 12-digit AWS account ids.",
		http.StatusForbidden: scannerHubForbiddenHint,
		http.StatusNotFound:  "account_id is not an account in Stream Security: it takes the Stream Security account id (streamsec_aws_account.<name>.id).",
		http.StatusConflict: `Another account's scanner already scans all AWS accounts (only one hub may cover "all"), or one of ` +
			"account_ids is already scanned by another hub. Change that hub's coverage first. " + pinnedAccountNote,
	},
}
