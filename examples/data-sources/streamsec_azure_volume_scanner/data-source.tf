# The collection token the scanner's jobs in the production subscription report
# and upload with. The volume-scanner module of terraform-streamsec-azure-tenant
# keeps it as a Container Apps job secret.
data "streamsec_azure_volume_scanner" "production" {
  tenant_id       = streamsec_azure_tenant.main.id
  subscription_id = "00000000-0000-0000-0000-000000000000"
}
