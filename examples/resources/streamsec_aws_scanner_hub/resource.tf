# The security account's volume scanner also scans the dev and prod accounts.
resource "streamsec_aws_scanner_hub" "security" {
  account_id  = streamsec_aws_account.security.id
  coverage    = "selected"
  account_ids = [streamsec_aws_account.dev.id, streamsec_aws_account.prod.id]
}

# The shared-services account's scanner scans every other AWS account that has no
# scanner of its own and no other hub. Only one hub may cover "all".
resource "streamsec_aws_scanner_hub" "shared_services" {
  account_id = streamsec_aws_account.shared_services.id
  coverage   = "all"
}
