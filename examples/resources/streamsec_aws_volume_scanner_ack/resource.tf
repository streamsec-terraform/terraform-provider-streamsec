# The volume-scanner module of terraform-streamsec-aws-account creates this once
# the scanner is installed in the region.
resource "streamsec_aws_volume_scanner_ack" "us_east_1" {
  account_id     = streamsec_aws_account.security.id
  region         = "us-east-1"
  module_version = "1.16.0"
}
