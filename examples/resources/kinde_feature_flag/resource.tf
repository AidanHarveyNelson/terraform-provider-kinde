resource "kinde_feature_flag" "beta_access" {
  key                  = "beta_access"
  name                 = "Beta access"
  description          = "Gives organizations access to beta features"
  type                 = "bool"
  allow_override_level = "org"
  default_value        = "false"
}
