variable "service_name" {
  description = "Name used for resource naming"
  type        = string
}

variable "container_port" {
  description = "Port to allow inbound traffic on"
  type        = number
}

variable "cidr_blocks" {
  description = "CIDR blocks allowed for inbound traffic"
  type        = list(string)
  default     = ["0.0.0.0/0"]
}
