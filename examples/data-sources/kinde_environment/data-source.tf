data "kinde_environment" "current" {}

output "environment_code" {
  value = data.kinde_environment.current.code
}
