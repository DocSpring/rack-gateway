package auth

import (
	"errors"
	"net/http"

	"github.com/DocSpring/rack-gateway/internal/gateway/netutil"
)

// ErrTooManyFailedAttempts is returned, without checking its credentials, for a request from a client that has
// used up its failed attempts.
var ErrTooManyFailedAttempts = errors.New("too many failed authentication attempts, try again later")

// RetryAfter is the Retry-After value (seconds) for a request refused with ErrTooManyFailedAttempts.
func (a *Service) RetryAfter() string {
	return a.retryAfter
}

// AuthenticateHTTPRequest attempts to authenticate the provided request, returning the user and auth source label.
// A client's failed attempts are limited, so credentials can't be guessed, or used to load the database, at the rate
// requests arrive: once a client has used them up, its requests are refused with ErrTooManyFailedAttempts before
// their credentials are checked, until they come back. A request without credentials isn't a failed attempt.
//
// A credential that authenticated recently, or a session the gateway just created (knownCredentials), isn't refused
// for its address's failed attempts, so a client sharing an address (a NAT, a proxy) isn't locked out by someone
// else's. It's still checked in full, and its attempts are limited on their own (see beginAttempt).
func (a *Service) AuthenticateHTTPRequest(r *http.Request) (*User, string, error) {
	credential := presentedCredential(r)
	end, ok := a.beginAttempt(netutil.ClientIP(r), credential)
	if !ok {
		return nil, "none", ErrTooManyFailedAttempts
	}
	user, source, err := a.authenticate(r)
	failed := err != nil && !errors.Is(err, ErrMissingAuthorization)
	end(failed)
	switch {
	case err == nil:
		a.known.remember(credential)
	case failed:
		a.known.forget(credential)
	}
	return user, source, err
}

// beginAttempt begins an attempt to authenticate with credential from the client at clientIP (see
// ratelimit.FailureLimiter.Begin). A known credential's attempts are limited by its own failed attempts instead of
// its address's, and also count as its address's when they fail. Its attempts still running count against it, so
// one that stops working while it's known (revoked or expired, say) gets no more attempts at once than an address
// does, however many are sent together, and is forgotten when the first of them fails.
func (a *Service) beginAttempt(clientIP, credential string) (end func(failed bool), ok bool) {
	if !a.known.has(credential) {
		return a.failures.Begin(clientIP)
	}
	endKnown, ok := a.knownFailures.Begin(credentialKey(credential))
	if !ok {
		return nil, false
	}
	return func(failed bool) {
		endKnown(failed)
		if failed {
			a.failures.Fail(clientIP)
		}
	}, true
}
