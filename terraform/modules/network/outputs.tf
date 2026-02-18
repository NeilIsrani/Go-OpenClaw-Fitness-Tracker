output "subnet_ids" {
  description = "Default VPC subnet IDs"
  value       = data.aws_subnets.default.ids
}

output "security_group_id" {
  description = "Security group ID for the service"
  value       = aws_security_group.service.id
}
