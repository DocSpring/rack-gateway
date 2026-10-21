package proxy

import (
	"context"
	"errors"
	"net/http"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
)

type envLockContextKey struct{}

// heldEnvLock carries the app env lock a release create takes (lockAppEnv) to the end of ProxyToRack, so the lock
// is held from reading the app's env until the rack has created the release.
type heldEnvLock struct {
	lock *db.AppEnvLock
}

// withEnvLockHolder gives the request somewhere to keep an app env lock, and returns the function that releases
// it.
func withEnvLockHolder(r *http.Request) (*http.Request, func()) {
	held := &heldEnvLock{}
	release := func() { held.lock.Release() }
	return r.WithContext(context.WithValue(r.Context(), envLockContextKey{}, held)), release
}

// lockAppEnv takes the app's env write lock (db.LockAppEnv) for the rest of the request. It returns
// db.ErrAppEnvLockBusy when another env change to the app holds it.
func (h *Handler) lockAppEnv(r *http.Request, app string) error {
	held, ok := r.Context().Value(envLockContextKey{}).(*heldEnvLock)
	if !ok {
		return errors.New("the request has no env lock holder")
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
