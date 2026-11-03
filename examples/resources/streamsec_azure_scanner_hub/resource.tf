# The security subscription's volume scanner also scans the dev and prod subscriptions
# of the tenant.
resource "streamsec_azure_scanner_hub" "security" {
  account_id       = streamsec_azure_tenant.this.id
  subscription_id  = "00000000-0000-0000-0000-000000000001"
  coverage         = "selected"
  subscription_ids = ["00000000-0000-0000-0000-000000000002", "00000000-0000-0000-0000-000000000003"]
}

# The shared-services subscription's scanner scans every other subscription of the
# tenant that has no scanner of its own and no other hub. Only one hub per tenant may
# cover "all".
resource "streamsec_azure_scanner_hub" "shared_services" {
  account_id      = streamsec_azure_tenant.this.id
  subscription_id = "00000000-0000-0000-0000-000000000004"
  coverage        = "all"
}
