# The security project's volume scanner also scans the dev and prod projects.
resource "streamsec_gcp_scanner_hub" "security" {
  account_id  = streamsec_gcp_project.security.id
  coverage    = "selected"
  account_ids = [streamsec_gcp_project.dev.id, streamsec_gcp_project.prod.id]
}

# The shared-services project's scanner scans every other GCP project that has no
# scanner of its own and no other hub. Only one hub may cover "all".
resource "streamsec_gcp_scanner_hub" "shared_services" {
  account_id = streamsec_gcp_project.shared_services.id
  coverage   = "all"
}
