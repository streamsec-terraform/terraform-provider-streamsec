# A Terraform install of the volume scanner in the production subscription
# creates this once the scanner's resources exist.
resource "streamsec_azure_volume_scanner_ack" "production" {
  account_id      = streamsec_azure_tenant.main.id
  subscription_id = "00000000-0000-0000-0000-000000000000"
  module_version  = "1.6.0"
  resource_group  = "streamsec-scanner-tf-00000000"
}
