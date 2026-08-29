resource "cloudblast_server" "example" {
  plan_id     = 19
  location_id = 2
  template    = "ubuntu-24.04"
  hostname    = "example-server"
}
