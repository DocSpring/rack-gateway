package proxy

import (
	"context"
	"errors"
	"net/http"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
)

type envLockContextKey struct{}

// heldEnvLock carries the app env lock a release create or a checked build takes (lockAppEnv) to the end of
// ProxyToRack, so the lock is held from reading the app's env until the rack has created the release or build.
type heldEnvLock struct {
	lock *db.AppEnvLock
	// checkedBuild is the checked_builds row of a build create whose manifest was checked with the app's env, and
	// buildNamed whether the rack's answer named the build.
	checkedBuild int64
	buildNamed   bool
}

// withEnvLockHolder gives the request somewhere to keep an app env lock, and returns the function that releases
// it.
func withEnvLockHolder(r *http.Request) (*http.Request, func()) {
	held := &heldEnvLock{}
	release := func() { held.lock.Release() }
	return r.WithContext(context.WithValue(r.Context(), envLockContextKey{}, held)), release
}

func envLockHolder(r *http.Request) (*heldEnvLock, error) {
	held, ok := r.Context().Value(envLockContextKey{}).(*heldEnvLock)
	if !ok {
		return nil, errors.New("the request has no env lock holder")
	}
	return held, nil
}

// lockAppEnv takes the app's env write lock (db.LockAppEnv) for the rest of the request. It returns
// db.ErrAppEnvLockBusy when another env change to the app holds it.
func (h *Handler) lockAppEnv(r *http.Request, app string) error {
	held, err := envLockHolder(r)
	if err != nil {
		return err
	}
	if h.database == nil {
		return errors.New("the app env lock needs the database")
	}
	lock, err := h.database.LockAppEnv(r.Context(), app)
	if err != nil {
		return err
	}
	held.lock.Release()
	held.lock = lock
	return nil
}

// recordCheckedBuild records a build create whose manifest was checked with the app's env, so the app's env can't
// change while the build may still read it (envutil.CheckNoBuildReadingEnv). The caller holds the app's env lock.
func (h *Handler) recordCheckedBuild(r *http.Request, app string) error {
	held, err := envLockHolder(r)
	if err != nil {
		return err
	}
	id, err := h.database.RecordCheckedBuild(app)
	if err != nil {
		return err
	}
	held.checkedBuild = id
	return nil
}

// nameCheckedBuild records the ID the rack gave a checked build. A build left without its ID holds up the app's
// env changes until db.CheckedBuildMaxAge.
func (h *Handler) nameCheckedBuild(r *http.Request, buildID string) {
	held, err := envLockHolder(r)
	if err != nil || held.checkedBuild == 0 {
		return
	}
	if err := h.database.SetCheckedBuildID(held.checkedBuild, buildID); err != nil {
		gtwlog.Errorf("checked build %s: %v", buildID, err)
		return
	}
	held.buildNamed = true
}

// settleCheckedBuild forgets a checked build the rack certainly didn't start: it refused the request (4xx), or
// never got it. One that may have started (no answer, a 5xx, a 2xx that named no build) is kept.
func (h *Handler) settleCheckedBuild(r *http.Request, statusCode int, forwardErr error) {
	held, err := envLockHolder(r)
	if err != nil || held.checkedBuild == 0 || held.buildNamed {
		return
	}
	refused := forwardErr == nil && statusCode >= 400 && statusCode < 500
	if !refused && (forwardErr == nil || !neverReachedRack(forwardErr)) {
		return
	}
	if err := h.database.FinishCheckedBuild(held.checkedBuild); err != nil {
		gtwlog.Errorf("failed to forget a build the rack didn't start: %v", err)
	}
}
