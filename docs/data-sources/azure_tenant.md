---
# generated by https://github.com/hashicorp/terraform-plugin-docs
page_title: "streamsec_azure_tenant Data Source - terraform-provider-streamsec"
subcategory: ""
description: |-
  AzureTenant data source
---

# streamsec_azure_tenant (Data Source)

AzureTenant data source



<!-- schema generated by tfplugindocs -->
## Schema

### Required

- `tenant_id` (String) The Azure tenant ID.

### Read-Only

- `account_token` (String) The account token of the tenant.
- `display_name` (String) The display name of the tenant.
- `id` (String) The internal ID of the account.
- `template_url` (String) The template URL of the tenant.
