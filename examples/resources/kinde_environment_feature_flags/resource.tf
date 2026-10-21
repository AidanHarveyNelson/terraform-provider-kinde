resource "kinde_environment_feature_flags" "this" {
  flags = {
    dark_mode = "true"
    max_seats = "25"
    theme     = "pink"
  }
}
