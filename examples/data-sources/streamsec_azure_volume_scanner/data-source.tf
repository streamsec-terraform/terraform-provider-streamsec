# The collection token the scanner's jobs in the production subscription report
# and upload with, for example as a Container Apps job secret.
data "streamsec_azure_volume_scanner" "production" {
  account_id      = streamsec_azure_tenant.main.id
  subscription_id = "00000000-0000-0000-0000-000000000000"

  depends_on = [streamsec_azure_tenant_ack.main]
}
