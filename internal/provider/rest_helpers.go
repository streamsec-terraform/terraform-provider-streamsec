package provider

import (
	"errors"
	"fmt"
	"net/http"

	"terraform-provider-streamsec/internal/client"

	"github.com/hashicorp/terraform-plugin-framework/diag"
)

// providerClient is the client the provider configured, for a resource's or data source's Configure.
func providerClient(providerData interface{}, diags *diag.Diagnostics) *client.Client {
	if providerData == nil {
		// The provider is not configured yet, as during validation.
		return nil
	}
	c, ok := providerData.(*client.Client)
	if !ok {
		diags.AddError(
			"Unexpected Configure Type",
			fmt.Sprintf("Expected *client.Client, got: %T. Please report this issue to the provider developers.", providerData),
		)
	}
	return c
}

// statusHints says what an HTTP status means for one call. Through the front gate a
// failed call arrives without ms_accounts' own reason, so the hint is all the user gets.
type statusHints map[int]string

func (h statusHints) detail(err error) string {
	var apiErr *client.APIError
	if errors.As(err, &apiErr) && h[apiErr.StatusCode] != "" {
		return h[apiErr.StatusCode] + "\n\n" + err.Error()
	}
	return err.Error()
}

func addAPIError(diags *diag.Diagnostics, summary string, err error, hints statusHints) {
	diags.AddError(summary, hints.detail(err))
}

// The statuses on which a destroy has nothing left to undo: the account is gone from
// Stream Security (404), or the hub was already turned off (409).
var goneStatuses = []int{http.StatusNotFound, http.StatusConflict}

// An uninstall acknowledgement only informs Stream Security, so a 403 must not block
// destroying the AWS resources either: an expired trial refuses changes, and a scoped
// token gets 403 for a deleted account.
var uninstallGoneStatuses = []int{http.StatusForbidden, http.StatusNotFound, http.StatusConflict}

// addDeleteError reports a failed destroy call. On one of the gone statuses it only
// warns: an error would block the destroy and keep the resource in state.
func addDeleteError(diags *diag.Diagnostics, errorSummary, goneSummary string, err error, hints statusHints, gone []int) {
	if hasStatus(err, gone...) {
		diags.AddWarning(goneSummary, "It is removed from the Terraform state only.\n\n"+hints.detail(err))
		return
	}
	addAPIError(diags, errorSummary, err, hints)
}

// The front gate refuses changes, but not reads, in a workspace whose trial has expired.
const trialExpiredHint = ", or the workspace's trial has expired (Stream Security then refuses changes)"

func hasStatus(err error, statuses ...int) bool {
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	for _, status := range statuses {
		if apiErr.StatusCode == status {
			return true
		}
	}
	return false
}
