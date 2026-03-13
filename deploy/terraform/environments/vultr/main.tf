terraform {
  required_version = ">= 1.6.0"

  required_providers {
    vultr = {
      source  = "vultr/vultr"
      version = ">= 2.0.0"
    }
  }
}

provider "vultr" {
  api_key = var.vultr_api_key
}

variable "vultr_api_key" {
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

variable "gateway_agent_sha256" {
  type        = string
  default     = ""
  description = "Optional checksum for gateway-agent binary."
}

variable "gateway_id" {
  type    = string
  default = "gw-lon-1"
}

variable "gateway_region_slug" {
  type    = string
  default = "eu-west"
}

variable "gateway_provider" {
  type    = string
  default = "vultr"
}

variable "vultr_region" {
  type    = string
  default = "lhr"
}

variable "vultr_plan" {
  type    = string
  default = "vc2-1c-1gb"
}

variable "vultr_os_id" {
  type        = number
  default     = 1743
  description = "Vultr OS ID (default is Ubuntu 24.04 x64 at time of writing)."
}

variable "ssh_key_ids" {
  type    = list(string)
  default = []
}

resource "vultr_instance" "gateway" {
  label       = var.gateway_id
  hostname    = var.gateway_id
  region      = var.vultr_region
  plan        = var.vultr_plan
  os_id       = var.vultr_os_id
  enable_ipv6 = true
  ssh_key_ids = var.ssh_key_ids

  user_data = templatefile("${path.module}/gateway-cloudinit.tftpl", {
    control_plane_base_url = var.control_plane_base_url
    gateway_id             = var.gateway_id
    gateway_region         = var.gateway_region_slug
    gateway_provider       = var.gateway_provider
    gateway_token          = var.gateway_token
    gateway_agent_url      = var.gateway_agent_url
    gateway_agent_sha256   = var.gateway_agent_sha256
    gateway_install_script_b64 = base64encode(file("${path.module}/../../../scripts/install-gateway.sh"))
  })

  tags = ["wg-platform", "gateway", "wireguard"]
}

output "gateway_id" {
  value = vultr_instance.gateway.id
}

output "gateway_main_ip" {
  value = vultr_instance.gateway.main_ip
}

output "gateway_v6_main_ip" {
  value = vultr_instance.gateway.v6_main_ip
}
