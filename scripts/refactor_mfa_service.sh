#!/bin/bash
set -e

base="internal/gateway/auth/mfa"
src="$base/service.go"

# Extract TOTP-related methods
output="$base/service_totp.go"
cat > "$output" << 'EOF'
package mfa

EOF

for func in "StartTOTPEnrollment" "ConfirmTOTP" "VerifyTOTP" "validateTOTPCodeWithTimeStep" "validateTOTPCode"; do
  ast-grep --pattern "func (s *Service) $func(\$\$\$) \$\$\$" "$src" --json=compact | \
    jq -r '.[0].text' >> "$output" 2>/dev/null || true
  echo -e "\n" >> "$output"
done

# Extract WebAuthn-related methods
output="$base/service_webauthn.go"
cat > "$output" << 'EOF'
package mfa

EOF

for func in "StartWebAuthnEnrollment" "ConfirmWebAuthnEnrollment" "StartWebAuthnAssertion" "VerifyWebAuthnAssertion" "VerifyWebAuthn"; do
  ast-grep --pattern "func (s *Service) $func(\$\$\$) \$\$\$" "$src" --json=compact | \
    jq -r '.[0].text' >> "$output" 2>/dev/null || true
  echo -e "\n" >> "$output"
done

# Extract webAuthnUser helper type and methods
for func in "WebAuthnID" "WebAuthnName" "WebAuthnDisplayName" "WebAuthnCredentials" "WebAuthnIcon"; do
  ast-grep --pattern "func (u *webAuthnUser) $func(\$\$\$) \$\$\$" "$src" --json=compact | \
    jq -r '.[0].text' >> "$output" 2>/dev/null || true
  echo -e "\n" >> "$output"
done

# Extract YubiOTP-related methods
output="$base/service_yubiotp.go"
cat > "$output" << 'EOF'
package mfa

EOF

for func in "StartYubiOTPEnrollment" "VerifyYubiOTP"; do
  ast-grep --pattern "func (s *Service) $func(\$\$\$) \$\$\$" "$src" --json=compact | \
    jq -r '.[0].text' >> "$output" 2>/dev/null || true
  echo -e "\n" >> "$output"
done

# Extract backup codes and trusted device methods
output="$base/service_recovery.go"
cat > "$output" << 'EOF'
package mfa

EOF

for func in "GenerateBackupCodes" "MintTrustedDevice" "ConsumeTrustedDevice" "generateBackupCodes" "hashBackupCode"; do
  ast-grep --pattern "func (s *Service) $func(\$\$\$) \$\$\$" "$src" --json=compact | \
    jq -r '.[0].text' >> "$output" 2>/dev/null || true
  echo -e "\n" >> "$output"
done

# Extract standalone functions (hashToken, hashUserAgent)
for func in "hashToken" "hashUserAgent"; do
  ast-grep --pattern "func $func(\$\$\$) \$\$\$" "$src" --json=compact | \
    jq -r '.[0].text' >> "$output" 2>/dev/null || true
  echo -e "\n" >> "$output"
done

echo "Extraction complete. Now creating core service file with struct and constructor..."

# Create core service file with struct, constructor, and shared methods
output="$base/service_core.go"
head -60 "$src" > "$output"
echo "" >> "$output"

# Extract NewService, IsWebAuthnConfigured, now, checkAndLockAccount
for func in "NewService" "IsWebAuthnConfigured" "now" "checkAndLockAccount"; do
  ast-grep --pattern "func $func(\$\$\$) \$\$\$" "$src" --json=compact | \
    jq -r '.[0].text' >> "$output" 2>/dev/null || \
  ast-grep --pattern "func (s *Service) $func(\$\$\$) \$\$\$" "$src" --json=compact | \
    jq -r '.[0].text' >> "$output" 2>/dev/null || true
  echo -e "\n" >> "$output"
done

echo "Fixing imports..."
goimports -w "$base"

echo "Verifying compilation..."
go build ./internal/gateway/auth/mfa

echo "✅ Success! Now manually review and delete $src after confirming everything works."
