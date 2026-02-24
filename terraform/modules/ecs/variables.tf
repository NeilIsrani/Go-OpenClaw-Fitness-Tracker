variable "service_name" {
  description = "Name used for resource naming"
  type        = string
}

variable "image" {
  description = "Docker image URI"
  type        = string
}

variable "container_port" {
  description = "Port the container listens on"
  type        = number
}

variable "subnet_ids" {
  description = "Subnet IDs for the ECS service"
  type        = list(string)
}

variable "security_group_ids" {
  description = "Security group IDs for the ECS service"
  type        = list(string)
}

variable "execution_role_arn" {
  description = "ARN of the ECS task execution role"
  type        = string
}

variable "task_role_arn" {
  description = "ARN of the ECS task role"
  type        = string
}

variable "log_group_name" {
  description = "CloudWatch log group name"
  type        = string
}

variable "ecs_count" {
  description = "Number of ECS tasks"
  type        = number
  default     = 1
}

variable "region" {
  description = "AWS region"
  type        = string
}

variable "cpu" {
  description = "CPU units for the task"
  type        = number
  default     = 256
}

variable "memory" {
  description = "Memory (MiB) for the task"
  type        = number
  default     = 512
}

variable "target_group_arn" {
  description = "ARN of the ALB target group to attach the service to"
  type        = string
}
