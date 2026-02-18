variable "service_name" {
  description = "Name used for log group naming"
  type        = string
}

variable "retention_in_days" {
  description = "Number of days to retain logs"
  type        = number
  default     = 7
}
