package cli

import (
	"os"
	"path/filepath"
)

// updateConfig applies change to the latest config and saves it, holding the config lock throughout, so two
// commands changing the config at once (a login and a logout, two logins) can't undo each other's changes. change
// returns false to leave the file as it is.
func updateConfig(change func(cfg *Config) (bool, error)) error {
	unlock, err := lockConfig()
	if err != nil {
		return err
	}
	defer unlock()

	cfg, _, err := readConfig()
	if err != nil {
		return err
	}
	defaulted := applyConfigDefaults(cfg)
	changed, err := change(cfg)
	if err != nil {
		return err
	}
	if !changed && !defaulted {
		return nil
	}
	return SaveConfig(cfg)
}

// lockConfig takes the exclusive lock on config.lock, next to config.json, waiting for any other command that
// holds it. The operating system releases the lock if the process exits without calling unlock.
func lockConfig() (func(), error) {
	if err := ensurePrivateDir(ConfigPath); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(filepath.Clean(ConfigPath), "config.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := lockFile(file); err != nil {
		_ = file.Close()
		return nil, err
	}
	return func() {
		_ = unlockFile(file)
		_ = file.Close()
	}, nil
}
