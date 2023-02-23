variable "project_id" {
  type        = string
  description = "GCP project id"
}

variable "region" {
  type        = string
  description = "GCP region"
}

variable "machine_type" {
  type        = string
  description = "specifies gcp instance type"
}

variable "public_keypath" {
  type        = string
  description = "Path for public key of gcp instance"
}

variable "private_keypath" {
  type        = string
  description = "Path for public key of gcp instance"
}