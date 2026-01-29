// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import starlightImageZoom from 'starlight-image-zoom';
import starlightLinksValidator from 'starlight-links-validator';

// https://astro.build/config
export default defineConfig({
  site: 'https://rack-gateway.docspring.io',
  integrations: [
    starlight({
      title: 'Rack Gateway',
      description: 'SOC 2 compliant authentication gateway for Convox racks',
      logo: {
        src: './src/assets/logo.svg',
        replacesTitle: true,
      },
      favicon: '/favicon.svg',
      social: [
        {
          icon: 'github',
          label: 'GitHub',
          href: 'https://github.com/docspring/rack-gateway',
        },
      ],
      editLink: {
        baseUrl: 'https://github.com/docspring/rack-gateway/edit/main/docs/',
      },
      plugins: [starlightImageZoom(), starlightLinksValidator()],
      customCss: ['./src/styles/global.css'],
      sidebar: [
        {
          label: 'Getting Started',
          items: [
            { label: 'Overview', slug: 'getting-started' },
            { label: 'What is Convox?', slug: 'getting-started/what-is-convox' },
            {
              label: 'What is Rack Gateway?',
              slug: 'getting-started/what-is-rack-gateway',
            },
            { label: 'Architecture', slug: 'getting-started/architecture' },
            { label: 'Quick Start', slug: 'getting-started/quick-start' },
            {
              label: 'System Requirements',
              slug: 'getting-started/system-requirements',
            },
          ],
        },
        {
          label: 'Concepts',
          items: [
            { label: 'Overview', slug: 'concepts' },
            {
              label: 'Authentication vs Authorization',
              slug: 'concepts/authentication-vs-authorization',
            },
            { label: 'OAuth 2.0 Explained', slug: 'concepts/oauth-explained' },
            { label: 'RBAC Principles', slug: 'concepts/rbac-principles' },
            {
              label: 'Zero Trust Security',
              slug: 'concepts/zero-trust-security',
            },
            { label: 'Audit Logging', slug: 'concepts/audit-logging' },
            { label: 'MFA Security', slug: 'concepts/mfa-security' },
            {
              label: 'Infrastructure Gateways',
              slug: 'concepts/infrastructure-gateways',
            },
          ],
        },
        {
          label: 'User Guide',
          items: [
            { label: 'Overview', slug: 'user-guide' },
            {
              label: 'CLI',
              collapsed: true,
              items: [
                { label: 'Overview', slug: 'user-guide/cli' },
                { label: 'Installation', slug: 'user-guide/cli/installation' },
                {
                  label: 'Authentication',
                  slug: 'user-guide/cli/authentication',
                },
                { label: 'Multi-Rack', slug: 'user-guide/cli/multi-rack' },
                { label: 'Commands', slug: 'user-guide/cli/commands' },
                {
                  label: 'MFA Verification',
                  slug: 'user-guide/cli/mfa-verification',
                },
              ],
            },
            {
              label: 'Web UI',
              collapsed: true,
              items: [
                { label: 'Overview', slug: 'user-guide/web-ui' },
                { label: 'Dashboard', slug: 'user-guide/web-ui/dashboard' },
                {
                  label: 'User Management',
                  slug: 'user-guide/web-ui/user-management',
                },
                { label: 'API Tokens', slug: 'user-guide/web-ui/api-tokens' },
                { label: 'Audit Logs', slug: 'user-guide/web-ui/audit-logs' },
                { label: 'Settings', slug: 'user-guide/web-ui/settings' },
              ],
            },
            {
              label: 'MFA',
              collapsed: true,
              items: [
                { label: 'Overview', slug: 'user-guide/mfa' },
                { label: 'TOTP Setup', slug: 'user-guide/mfa/totp-setup' },
                { label: 'WebAuthn', slug: 'user-guide/mfa/webauthn' },
                { label: 'YubiKey', slug: 'user-guide/mfa/yubikey' },
                {
                  label: 'Trusted Devices',
                  slug: 'user-guide/mfa/trusted-devices',
                },
                { label: 'Backup Codes', slug: 'user-guide/mfa/backup-codes' },
              ],
            },
          ],
        },
        {
          label: 'Configuration',
          items: [
            { label: 'Overview', slug: 'configuration' },
            {
              label: 'Environment Variables',
              slug: 'configuration/environment-variables',
            },
            { label: 'OAuth Setup', slug: 'configuration/oauth-setup' },
            {
              label: 'Session Management',
              slug: 'configuration/session-management',
            },
            {
              label: 'Security Settings',
              slug: 'configuration/security-settings',
            },
            {
              label: 'Email Notifications',
              slug: 'configuration/email-notifications',
            },
          ],
        },
        {
          label: 'Security',
          items: [
            { label: 'Overview', slug: 'security' },
            {
              label: 'RBAC',
              collapsed: true,
              items: [
                { label: 'Overview', slug: 'security/rbac' },
                { label: 'Roles', slug: 'security/rbac/roles' },
                { label: 'Permissions', slug: 'security/rbac/permissions' },
                {
                  label: 'Best Practices',
                  slug: 'security/rbac/best-practices',
                },
              ],
            },
            {
              label: 'Authentication',
              collapsed: true,
              items: [
                { label: 'Overview', slug: 'security/authentication' },
                { label: 'OAuth Flow', slug: 'security/authentication/oauth-flow' },
                { label: 'Sessions', slug: 'security/authentication/sessions' },
                {
                  label: 'API Tokens',
                  slug: 'security/authentication/api-tokens',
                },
              ],
            },
            {
              label: 'Compliance',
              collapsed: true,
              items: [
                { label: 'Overview', slug: 'security/compliance' },
                { label: 'SOC 2', slug: 'security/compliance/soc2' },
                { label: 'Audit Trail', slug: 'security/compliance/audit-trail' },
                {
                  label: 'Data Retention',
                  slug: 'security/compliance/data-retention',
                },
              ],
            },
            { label: 'Hardening', slug: 'security/hardening' },
          ],
        },
        {
          label: 'Deployment',
          items: [
            { label: 'Overview', slug: 'deployment' },
            { label: 'Docker', slug: 'deployment/docker' },
            { label: 'Convox', slug: 'deployment/convox' },
            {
              label: 'Private Network',
              slug: 'deployment/private-network',
            },
            {
              label: 'Terraform',
              collapsed: true,
              items: [
                { label: 'Overview', slug: 'deployment/terraform' },
                {
                  label: 'AWS Infrastructure',
                  slug: 'deployment/terraform/aws-infrastructure',
                },
                {
                  label: 'S3 WORM Storage',
                  slug: 'deployment/terraform/s3-worm-storage',
                },
                {
                  label: 'Multi-Region',
                  slug: 'deployment/terraform/multi-region',
                },
              ],
            },
            { label: 'Database Setup', slug: 'deployment/database-setup' },
            {
              label: 'Production Checklist',
              slug: 'deployment/production-checklist',
            },
          ],
        },
        {
          label: 'Integrations',
          items: [
            { label: 'Overview', slug: 'integrations' },
            {
              label: 'Deploy Approvals',
              collapsed: true,
              items: [
                { label: 'Overview', slug: 'integrations/deploy-approvals' },
                {
                  label: 'CircleCI',
                  slug: 'integrations/deploy-approvals/circleci',
                },
                {
                  label: 'GitHub',
                  slug: 'integrations/deploy-approvals/github',
                },
                {
                  label: 'Workflow',
                  slug: 'integrations/deploy-approvals/workflow',
                },
              ],
            },
            { label: 'Slack', slug: 'integrations/slack' },
            { label: 'Email', slug: 'integrations/email' },
          ],
        },
        {
          label: 'Operations',
          items: [
            { label: 'Overview', slug: 'operations' },
            { label: 'Monitoring', slug: 'operations/monitoring' },
            {
              label: 'Database Maintenance',
              slug: 'operations/database-maintenance',
            },
            { label: 'Troubleshooting', slug: 'operations/troubleshooting' },
            { label: 'Upgrades', slug: 'operations/upgrades' },
          ],
        },
        {
          label: 'Development',
          items: [
            { label: 'Overview', slug: 'development' },
            { label: 'Local Setup', slug: 'development/local-setup' },
            { label: 'Testing', slug: 'development/testing' },
            { label: 'Contributing', slug: 'development/contributing' },
            { label: 'API Reference', slug: 'development/api-reference' },
          ],
        },
        {
          label: 'Reference',
          items: [
            { label: 'Overview', slug: 'reference' },
            { label: 'CLI Commands', slug: 'reference/cli-commands' },
            {
              label: 'Environment Variables',
              slug: 'reference/environment-variables',
            },
            { label: 'RBAC Permissions', slug: 'reference/rbac-permissions' },
            { label: 'Audit Events', slug: 'reference/audit-events' },
            { label: 'API Endpoints', slug: 'reference/api-endpoints' },
          ],
        },
      ],
    }),
  ],
});
