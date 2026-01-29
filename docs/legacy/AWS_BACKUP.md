AWS Backup for RDS (Long-Term Retention)

Overview
- Convox can provision an RDS Postgres instance with automated backups (up to 35 days).
- For longer retention (12 months + 7 yearly), use AWS Backup with a Backup Vault and Backup Plan.

Key points
- Daily: retain 7 days
- Weekly: retain 4 weeks
- Monthly: retain 12 months
- Yearly: retain 7 years

Example Terraform (attach to one RDS per environment)

```
resource "aws_backup_vault" "gateway" {
  name        = "gateway-backup-vault"
  kms_key_arn = aws_kms_key.backup.arn
}

resource "aws_backup_plan" "gateway" {
  name = "gateway-backup-plan"

  rule {
    rule_name         = "daily"
    target_vault_name = aws_backup_vault.gateway.name
    schedule          = "cron(0 3 * * ? *)" # 03:00 UTC daily
    lifecycle {
      delete_after = 7 # days
    }
  }

  rule {
    rule_name         = "weekly"
    target_vault_name = aws_backup_vault.gateway.name
    schedule          = "cron(0 4 ? * SUN *)" # Sundays
    lifecycle {
      delete_after = 30 # approx 4 weeks
    }
  }

  rule {
    rule_name         = "monthly"
    target_vault_name = aws_backup_vault.gateway.name
    schedule          = "cron(0 5 1 * ? *)" # 1st of the month
    lifecycle {
      delete_after = 365 # 12 months
    }
  }

  rule {
    rule_name         = "yearly"
    target_vault_name = aws_backup_vault.gateway.name
    schedule          = "cron(0 6 1 1 ? *)" # Jan 1
    lifecycle {
      delete_after = 2555 # ~7 years
    }
  }
}

# Attach RDS instance to backup plan
resource "aws_backup_selection" "gateway_rds" {
  iam_role_arn = aws_iam_role.backup_service_role.arn
  name         = "gateway-rds-selection"
  plan_id      = aws_backup_plan.gateway.id

  resources = [
    aws_db_instance.pg.arn,
  ]
}
```

Notes
- Replace schedules and retention values to match your policy exactly.
- Ensure the IAM role grants AWS Backup permissions for RDS.
- Create one plan/selection per environment (US, EU, staging) to keep isolation.

