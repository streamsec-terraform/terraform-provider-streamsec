# A Terraform install of the volume scanner in the production subscription
# creates this once the scanner's resources exist. Stream Security takes it only
# once the tenant itself is acknowledged.
resource "streamsec_azure_volume_scanner_ack" "production" {
  account_id      = streamsec_azure_tenant.main.id
  subscription_id = "00000000-0000-0000-0000-000000000000"
  module_version  = "1.6.0"
  resource_group  = "streamsec-scanner-tf-00000000"

  # The scanner's managed identity, for the console's access template when the
  # scanner is a hub (streamsec_azure_scanner_hub).
  principal_id = "00000000-0000-0000-0000-00000000000a"

  depends_on = [streamsec_azure_tenant_ack.main]
}
