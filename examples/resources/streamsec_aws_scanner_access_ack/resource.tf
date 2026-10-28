# The volume-scanner-access module of terraform-streamsec-aws-account creates this
# in the spoke account once its StreamScannerSpokeRole exists.
resource "streamsec_aws_scanner_access_ack" "dev" {
  hub_account_id         = streamsec_aws_scanner_hub.security.account_id
  spoke_cloud_account_id = "444455556666"

  depends_on = [aws_iam_role.stream_scanner_spoke]
}
