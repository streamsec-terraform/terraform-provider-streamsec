# What the dev account's StreamScannerSpokeRole must trust for the security
# account's scanner to scan it. The hub must exist first: in the same
# configuration, depend on it. The volume-scanner-access module of
# terraform-streamsec-aws-account builds the whole role from this.
data "streamsec_aws_scanner_hub_access" "security" {
  hub_account_id = streamsec_aws_scanner_hub.security.account_id

  depends_on = [streamsec_aws_scanner_hub.security]
}

locals {
  hub = data.streamsec_aws_scanner_hub_access.security
}

resource "aws_iam_role" "stream_scanner_spoke" {
  name = local.hub.role_name

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { AWS = "arn:aws:iam::${local.hub.hub_cloud_account_id}:root" }
      Action    = "sts:AssumeRole"
      Condition = {
        StringEquals = { "sts:ExternalId" = local.hub.external_id }
        StringLike = {
          "aws:PrincipalArn" = [
            for pattern in local.hub.trusted_role_name_patterns :
            "arn:aws:iam::${local.hub.hub_cloud_account_id}:role/${pattern}"
          ]
        }
      }
    }]
  })
}
