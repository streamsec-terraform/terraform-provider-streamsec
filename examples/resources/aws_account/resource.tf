resource "streamsec_aws_account" "test" {
  cloud_account_id = "123456789011"
  display_name     = "asdsadasdas"
  stack_region     = "us-east-1"
  cloud_regions    = ["us-east-1"]
}