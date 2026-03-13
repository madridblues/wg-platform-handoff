terraform {
  required_version = ">= 1.6.0"

  required_providers {
    hcloud = {
      source  = "hetznercloud/hcloud"
      version = "~> 1.51"
    }
  }
}

provider "hcloud" {
  token = var.hcloud_token
}

variable "hcloud_token" {
  type      = string
  sensitive = true
}

variable "gateway_token" {
  type      = string
  sensitive = true
}

variable "control_plane_base_url" {
  type = string
}

variable "gateway_agent_url" {
  type = string
}

variable "gateway_install_script_url" {
  type        = string
  description = "URL to install-gateway.sh pinned to a trusted commit/tag."
}

variable "gateway_agent_sha256" {
  type        = string
  default     = ""
  description = "Optional checksum for gateway-agent binary."
}

variable "gateway_region_slug" {
  type    = string
  default = "eu-west"
}

variable "hcloud_location" {
  type    = string
  default = "nbg1"
}

variable "hcloud_server_type" {
  type    = string
  default = "cpx21"
}

variable "hcloud_image" {
  type    = string
  default = "ubuntu-24.04"
}

variable "ssh_key_ids" {
  type    = list(string)
  default = []
}

variable "gateway_provider" {
  type    = string
  default = "hetzner"
}

resource "hcloud_server" "gateway_lon_1" {
  name        = "gw-lon-1"
  server_type = var.hcloud_server_type
  image       = var.hcloud_image
  location    = var.hcloud_location
  ssh_keys    = var.ssh_key_ids

  user_data = templatefile("${path.module}/gateway-cloudinit.tftpl", {
    control_plane_base_url  = var.control_plane_base_url
    gateway_id              = "gw-lon-1"
    gateway_region          = var.gateway_region_slug
    gateway_provider        = var.gateway_provider
    gateway_token           = var.gateway_token
    gateway_agent_url       = var.gateway_agent_url
    gateway_agent_sha256    = var.gateway_agent_sha256
    gateway_install_script  = var.gateway_install_script_url
  })
}

output "gateway_ipv4" {
  value = hcloud_server.gateway_lon_1.ipv4_address
}

output "gateway_ipv6" {
  value = hcloud_server.gateway_lon_1.ipv6_address
}

output "gateway_name" {
  value = hcloud_server.gateway_lon_1.name
}
