resource "kinde_organization" "acme" {
  name = "Acme"
}

resource "kinde_feature_flag" "beta_access" {
  key                  = "beta_access"
  name                 = "Beta access"
  type                 = "bool"
  allow_override_level = "org"
  default_value        = "false"
}

resource "kinde_organization_feature_flags" "acme" {
  org_code = kinde_organization.acme.code
  flags = {
    (kinde_feature_flag.beta_access.key) = "true"
  }
}
