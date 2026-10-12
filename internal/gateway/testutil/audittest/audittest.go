// Package audittest simulates an audit log outage for tests: give an audit.Logger a FailingChainWriter with
// SetChainWriter and every write fails, as when the audit database is down.
package audittest

import (
	"errors"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

// ErrUnavailable is the error FailingChainWriter returns.
var ErrUnavailable = errors.New("audit database unavailable (test)")

// FailingChainWriter is an audit.ChainWriter whose every write fails.
type FailingChainWriter struct{}

// CreateAuditLog fails.
func (FailingChainWriter) CreateAuditLog(*db.AuditLog) error {
	return ErrUnavailable
}
