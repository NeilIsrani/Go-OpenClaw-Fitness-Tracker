resource "aws_iam_role" "ecs_execution_role" {
  name = "${var.service_name}-ecs-execution-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action    = "sts:AssumeRole"
      Effect    = "Allow"
      Principal = { Service = "ecs-tasks.amazonaws.com" }
    }]
  })
}

resource "aws_iam_role_policy_attachment" "ecs_execution_policy" {
  role       = aws_iam_role.ecs_execution_role.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"
}

# --- Modules ---

module "network" {
  source         = "./modules/network"
  service_name   = var.service_name
  container_port = var.container_port
}

module "ecr" {
  source          = "./modules/ecr"
  repository_name = var.ecr_repository_name
}

module "logging" {
  source            = "./modules/logging"
  service_name      = var.service_name
  retention_in_days = var.log_retention_days
}

module "ecs" {
  source             = "./modules/ecs"
  service_name       = var.service_name
  image              = docker_registry_image.app.name
  container_port     = var.container_port
  subnet_ids         = module.network.subnet_ids
  security_group_ids = [module.network.security_group_id]
  execution_role_arn = aws_iam_role.ecs_execution_role.arn
  task_role_arn      = aws_iam_role.ecs_execution_role.arn
  log_group_name     = module.logging.log_group_name
  ecs_count          = var.ecs_count
  region             = var.aws_region
  target_group_arn   = module.network.target_group_arn
}

# --- Docker Image Build & Push ---

resource "docker_image" "app" {
  name = "${module.ecr.repository_url}:latest"

  build {
    context    = "../src"
    dockerfile = "Dockerfile"
    platform   = "linux/amd64"
  }

  triggers = {
    dir_sha1 = sha1(join("", [for f in fileset("../src", "**") : filesha1("../src/${f}")]))
  }
}

resource "docker_registry_image" "app" {
  name          = docker_image.app.name
  keep_remotely = true
}
