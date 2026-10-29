# The volume-scanner module of terraform-streamsec-azure-tenant creates this once
# the scanner is installed in the subscription.
resource "streamsec_azure_volume_scanner_ack" "production" {
  tenant_id       = streamsec_azure_tenant.main.id
  subscription_id = "00000000-0000-0000-0000-000000000000"
  module_version  = "1.6.0"
  resource_group  = "streamsec-scanner-tf-00000000"
}
