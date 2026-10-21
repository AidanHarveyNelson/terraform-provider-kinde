resource "kinde_feature_flag" "max_projects" {
  key                  = "max_projects"
  name                 = "Max projects"
  type                 = "int"
  allow_override_level = "usr"
  default_value        = "3"
}

resource "kinde_user_feature_flags" "jane" {
  user_id = "kp_1234567890abcdef"
  flags = {
    (kinde_feature_flag.max_projects.key) = "10"
  }
}
