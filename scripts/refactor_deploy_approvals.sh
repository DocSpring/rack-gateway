#!/bin/bash
set -e

base="internal/gateway/handlers"
src="$base/deploy_approval_requests.go"

# Extract APIHandler methods (Create, Get)
output="$base/deploy_approval_create.go"
cat > "$output" << 'EOF'
package handlers

import (
	"github.com/gin-gonic/gin"
)

EOF

for func in "CreateDeployApprovalRequest" "GetDeployApprovalRequest"; do
  ast-grep --pattern "func (h *APIHandler) $func(\$\$\$) \$\$\$" "$src" --json=compact | \
    jq -r '.[0].text' >> "$output"
  echo -e "\n" >> "$output"
done

# Extract AdminHandler methods (List, Approve, Reject, GetAuditLogs)
output="$base/deploy_approval_admin.go"
cat > "$output" << 'EOF'
package handlers

import (
	"github.com/gin-gonic/gin"
)

EOF

for func in "ListDeployApprovalRequests" "ApproveDeployApprovalRequest" "RejectDeployApprovalRequest" "GetDeployApprovalRequestAuditLogs"; do
  ast-grep --pattern "func (h *AdminHandler) $func(\$\$\$) \$\$\$" "$src" --json=compact | \
    jq -r '.[0].text' >> "$output"
  echo -e "\n" >> "$output"
done

# Extract helper functions
output="$base/deploy_approval_helpers.go"
cat > "$output" << 'EOF'
package handlers

import (
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

EOF

for func in "resolveDeployApprovalRequestToken" "toDeployApprovalRequestResponse" "auditDetails" "getAppSettingBool" "getAppSettingString"; do
  ast-grep --pattern "func $func(\$\$\$) \$\$\$" "$src" --json=compact | \
    jq -r '.[0].text' >> "$output"
  echo -e "\n" >> "$output"
done

echo "Extraction complete. Now fixing imports..."
goimports -w "$base"

echo "Verifying compilation..."
go build ./internal/gateway/handlers

echo "Success! You can now delete $src"
