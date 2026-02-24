output "subnet_ids" {
  description = "Default VPC subnet IDs"
  value       = data.aws_subnets.default.ids
}

output "security_group_id" {
  description = "Security group ID for the service"
  value       = aws_security_group.service.id
}

output "alb_dns_name" {
  description = "Public DNS name of the ALB"
  value       = aws_lb.alb.dns_name
}

output "target_group_arn" {
  description = "ARN of the ALB target group"
  value       = aws_lb_target_group.tg.arn
}
