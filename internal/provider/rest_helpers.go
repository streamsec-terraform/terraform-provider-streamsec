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

// addDeleteError reports a failed destroy call. An account gone from Stream Security,
// or a hub already turned off (404, 409), has nothing left to undo, so it only
// warns: an error would block the destroy and keep the resource in state.
func addDeleteError(diags *diag.Diagnostics, errorSummary, goneSummary string, err error, hints statusHints) {
	if hasStatus(err, http.StatusNotFound, http.StatusConflict) {
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
