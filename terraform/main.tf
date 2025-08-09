terraform {
  required_version = ">= 1.0"
  
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

variable "environment" {
  description = "Environment name (staging, production, etc)"
  type        = string
}

variable "convox_rack" {
  description = "Convox rack name"
  type        = string
}

variable "google_client_id" {
  description = "Google OAuth client ID"
  type        = string
  sensitive   = true
}

variable "google_client_secret" {
  description = "Google OAuth client secret"
  type        = string
  sensitive   = true
}

variable "rack_tokens" {
  description = "Convox rack API tokens"
  type        = map(string)
  sensitive   = true
}

variable "admin_users" {
  description = "Comma-separated list of admin user emails"
  type        = string
  default     = ""
}

variable "domain" {
  description = "Domain for the auth proxy"
  type        = string
}

resource "aws_kms_key" "convox_gateway" {
  description             = "KMS key for Convox Gateway secrets"
  deletion_window_in_days = 10
  
  tags = {
    Name        = "convox-gateway-${var.environment}"
    Environment = var.environment
    ManagedBy   = "terraform"
  }
}

resource "aws_kms_alias" "convox_gateway" {
  name          = "alias/convox-gateway-${var.environment}"
  target_key_id = aws_kms_key.convox_gateway.key_id
}

resource "aws_cloudwatch_log_group" "convox_gateway" {
  name              = "/convox/${var.convox_rack}/convox-gateway"
  retention_in_days = 90
  kms_key_id        = aws_kms_key.convox_gateway.arn

  tags = {
    Application = "convox-gateway"
    Environment = var.environment
    ManagedBy   = "terraform"
  }
}

resource "random_password" "jwt_key" {
  length  = 64
  special = true
}

resource "aws_ssm_parameter" "jwt_key" {
  name  = "/convox-gateway/${var.environment}/jwt-key"
  type  = "SecureString"
  value = random_password.jwt_key.result
  key_id = aws_kms_key.convox_gateway.key_id

  tags = {
    Application = "convox-gateway"
    Environment = var.environment
  }
}

resource "aws_ssm_parameter" "google_client_id" {
  name  = "/convox-gateway/${var.environment}/google-client-id"
  type  = "SecureString"
  value = var.google_client_id
  key_id = aws_kms_key.convox_gateway.key_id

  tags = {
    Application = "convox-gateway"
    Environment = var.environment
  }
}

resource "aws_ssm_parameter" "google_client_secret" {
  name  = "/convox-gateway/${var.environment}/google-client-secret"
  type  = "SecureString"
  value = var.google_client_secret
  key_id = aws_kms_key.convox_gateway.key_id

  tags = {
    Application = "convox-gateway"
    Environment = var.environment
  }
}

resource "aws_ssm_parameter" "rack_tokens" {
  for_each = var.rack_tokens

  name  = "/convox-gateway/${var.environment}/rack-token-${each.key}"
  type  = "SecureString"
  value = each.value
  key_id = aws_kms_key.convox_gateway.key_id

  tags = {
    Application = "convox-gateway"
    Environment = var.environment
    Rack        = each.key
  }
}

output "kms_key_id" {
  value = aws_kms_key.convox_gateway.id
}

output "log_group_name" {
  value = aws_cloudwatch_log_group.convox_gateway.name
}

output "ssm_parameter_names" {
  value = {
    jwt_key              = aws_ssm_parameter.jwt_key.name
    google_client_id     = aws_ssm_parameter.google_client_id.name
    google_client_secret = aws_ssm_parameter.google_client_secret.name
    rack_tokens          = { for k, v in aws_ssm_parameter.rack_tokens : k => v.name }
  }
}