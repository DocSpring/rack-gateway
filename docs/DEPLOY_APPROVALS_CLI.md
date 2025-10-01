# Deploy Approval CLI UX Improvements

## Current Issues

1. **Manual ID lookup required**: Admin has to find the request ID separately before approving
2. **No interactive selection**: Can't browse pending requests in the CLI
3. **Integer IDs exposed**: Using database integer IDs instead of UUIDs
4. **Poor UX for MFA**: Admin prompted for code even when using WebAuthn

## Desired UX

### Scenario 1: Approve Specific Request (by ID)

```bash
convox-gateway deploy-approval approve <uuid>
```

**Flow:**
1. Fetch request details by UUID
2. Display request info (API token name, app, release, message, etc.)
3. Prompt for MFA:
   - If WebAuthn available: "Touch your security key to approve..."
   - If TOTP only: "Enter your MFA code:"
4. Submit approval with MFA verification
5. Show success message

### Scenario 2: Interactive Approval (no ID specified)

```bash
convox-gateway deploy-approval approve
```

**Flow:**
1. Fetch all pending requests
2. **If zero requests:**
   ```
   No pending deploy approval requests.
   ```
   Exit 0

3. **If one request:**
   Skip directly to step 4

4. **If multiple requests:**
   Show interactive selector (fzf-style):
   ```
   Select deploy approval request (type to filter, ↑↓ to navigate, Enter to select):

   > req_abc123  docspring-api-deploy  DocSpring API   "Deploy v2.3.1 to production"
     req_def456  stripe-sync-token     Stripe Sync     "Update Stripe plans"
     req_ghi789  billing-processor     Billing Service "Fix invoice generation bug"
   ```

5. **Show request details:**
   ```
   Deploy Approval Request: req_abc123

   API Token:    docspring-api-deploy (tok_xyz789)
   Created:      2 minutes ago
   App:          docspring-api
   Release:      r-abc123def456
   Message:      "Deploy v2.3.1 to production"

   Created by:   deploy-bot@example.com (API Token: ci-deploy-token)
   ```

6. **Prompt for approval:**
   ```
   Approve this request? [y/N]: y
   ```

7. **MFA verification:**
   - If WebAuthn: `Touch your security key to approve...` (auto-detects device)
   - If TOTP: `Enter your MFA code: ______`

8. **Optional notes:**
   ```
   Add approval notes (optional, press Enter to skip): Approved after verifying tests passed
   ```

9. **Submit and confirm:**
   ```
   ✓ Deploy approval request req_abc123 approved successfully.
   ```

## Implementation Tasks

### 1. Add UUID to deploy_approval_requests table

**Migration:** `internal/gateway/db/migrations/XXXXXX_add_uuid_to_deploy_approvals.sql`

```sql
-- Add public_id column (UUID)
ALTER TABLE deploy_approval_requests
  ADD COLUMN public_id TEXT UNIQUE;

-- Generate UUIDs for existing rows
UPDATE deploy_approval_requests
  SET public_id = 'req_' || substr(md5(random()::text || id::text), 1, 20)
  WHERE public_id IS NULL;

-- Make it NOT NULL
ALTER TABLE deploy_approval_requests
  ALTER COLUMN public_id SET NOT NULL;

-- Add index
CREATE INDEX idx_deploy_approval_requests_public_id
  ON deploy_approval_requests(public_id);
```

**Update model:** `internal/gateway/db/types.go`
```go
type DeployApprovalRequest struct {
    ID       int64  `json:"-"`  // Hide internal ID
    PublicID string `json:"id"`  // UUID for external use
    // ... rest of fields
}
```

### 2. Update Gateway API to use UUIDs

**Files to modify:**
- `internal/gateway/handlers/deploy_approvals.go` - Accept UUID in routes
- `internal/gateway/routes/routes.go` - Change routes from `/id/:id` to `/id/:public_id`
- `internal/gateway/db/deploy_approvals.go` - Add `GetByPublicID()` method

### 3. Create Interactive Selector for CLI

**New file:** `internal/cli/selector/selector.go`

```go
package selector

import (
    "fmt"
    "os"
    "strings"
)

type Item struct {
    ID          string
    DisplayName string
    Description string
}

// Select shows an interactive fzf-style selector
// Returns selected item or error
func Select(items []Item, prompt string) (*Item, error) {
    // Implementation options:
    // 1. Use github.com/manifoldco/promptui (simple, no external deps)
    // 2. Use github.com/charmbracelet/bubbletea (fancy, TUI framework)
    // 3. Shell out to fzf if available (fast, but external dependency)
    //
    // Recommended: manifoldco/promptui for simplicity
}
```

**Example usage:**
```go
items := make([]selector.Item, len(requests))
for i, req := range requests {
    items[i] = selector.Item{
        ID:          req.PublicID,
        DisplayName: fmt.Sprintf("%-15s %-20s", req.PublicID, req.TargetAPITokenName),
        Description: req.Message,
    }
}

selected, err := selector.Select(items, "Select deploy approval request:")
if err != nil {
    return err
}

// selected.ID contains the UUID
```

### 4. Update CLI Approve Command

**File:** `cmd/cli/deploy_approvals.go`

**Current function:** `approveDeployApproval(id int64, notes string)`

**New function signature:**
```go
func approveDeployApproval(publicID string, notes string) error
```

**Logic:**
```go
func approveCommand(args []string) error {
    var selectedID string
    var notes string

    // Parse flags
    fs := flag.NewFlagSet("approve", flag.ExitOnError)
    fs.StringVar(&notes, "notes", "", "Approval notes")
    fs.Parse(args)

    remainingArgs := fs.Args()

    if len(remainingArgs) > 0 {
        // Case 1: ID specified
        selectedID = remainingArgs[0]
    } else {
        // Case 2: Interactive selection
        requests, err := fetchPendingRequests()
        if err != nil {
            return err
        }

        if len(requests) == 0 {
            fmt.Println("No pending deploy approval requests.")
            return nil
        }

        if len(requests) == 1 {
            selectedID = requests[0].PublicID
        } else {
            // Show interactive selector
            items := buildSelectorItems(requests)
            selected, err := selector.Select(items, "Select deploy approval request:")
            if err != nil {
                return err
            }
            selectedID = selected.ID
        }
    }

    // Fetch and display request details
    request, err := fetchRequestByID(selectedID)
    if err != nil {
        return err
    }

    displayRequestDetails(request)

    // Confirm approval
    if !confirmApproval() {
        fmt.Println("Approval cancelled.")
        return nil
    }

    // Prompt for notes if not provided via flag
    if notes == "" {
        notes = promptForNotes()
    }

    // MFA verification (WebAuthn or TOTP)
    // This should use the same performMFAVerification flow as login
    if err := verifyMFAForApproval(); err != nil {
        return err
    }

    // Submit approval
    return submitApproval(selectedID, notes)
}
```

### 5. MFA Integration for Approvals

The approval flow should reuse the MFA verification from login:

```go
func verifyMFAForApproval(gatewayURL, sessionToken string) error {
    // Get MFA status
    status, err := getMFAStatus(gatewayURL, sessionToken)
    if err != nil {
        return err
    }

    // Try WebAuthn first if available
    if hasWebAuthnMethod(status.Methods) && webauthn.CheckAvailability() {
        fmt.Println("Touch your security key to approve...")
        // Get assertion and submit
        return verifyWithWebAuthn(gatewayURL, sessionToken)
    }

    // Fall back to TOTP
    fmt.Print("Enter your MFA code: ")
    code, err := readMFACode()
    if err != nil {
        return err
    }

    return verifyWithTOTP(gatewayURL, sessionToken, code)
}
```

## Dependencies to Add

```bash
go get github.com/manifoldco/promptui
```

Alternative (more features, larger):
```bash
go get github.com/charmbracelet/bubbletea
go get github.com/charmbracelet/bubbles
```

## Testing Checklist

- [ ] List pending requests (0, 1, many)
- [ ] Interactive selector with filtering
- [ ] Approve by UUID
- [ ] MFA with WebAuthn
- [ ] MFA with TOTP fallback
- [ ] Add approval notes
- [ ] Error handling (invalid UUID, request already approved, etc.)
- [ ] Display request details correctly
- [ ] UUID generation for new requests
- [ ] Migration runs successfully on existing data

## Future Enhancements

- Add `convox-gateway deploy-approval reject <uuid>` with same UX
- Add `convox-gateway deploy-approval list --status pending|approved|rejected`
- Show diff/changes in the request details
- Add colors and better formatting with lipgloss
- Auto-refresh when new requests come in
