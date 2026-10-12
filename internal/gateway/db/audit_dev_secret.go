package db

import "strings"

// IsDevelopmentAuditHMACSecret reports whether secret is the development audit secret published in this repository
// (the writer's fallback when AUDIT_HMAC_SECRET is unset), however it is spaced. A deployment must never use it:
// anyone could recompute the audit log's hashes with it.
func IsDevelopmentAuditHMACSecret(secret string) bool {
	return strings.TrimSpace(secret) == devAuditHMACSecret
}
