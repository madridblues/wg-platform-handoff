terraform {
  required_version = ">= 1.6.0"
}

variable "gateway_name" {
  type = string
}

variable "region" {
  type = string
}

variable "control_plane_base_url" {
  type = string
}

variable "gateway_token" {
  type      = string
  sensitive = true
}

# TODO: Implement provider-specific resources in consumer environment (Hetzner/AWS/DO).
# This module is intentionally provider-agnostic as a handoff scaffold.

output "gateway_name" {
  value = var.gateway_name
}
