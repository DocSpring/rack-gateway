package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	sentry "github.com/getsentry/sentry-go"

	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	gtwlog "github.com/DocSpring/rack-gateway/internal/gateway/logging"
)

// columnUpgradeRepeatDelay is when the column encryption upgrade runs again after startup: after a rolling
// deploy has replaced the previous release, which may have written plaintext secrets meanwhile.
const columnUpgradeRepeatDelay = 10 * time.Minute

// upgradePlaintextColumns encrypts secrets stored before column encryption (v0.1.7), before anything reads
// them. During a rolling deploy the previous release can still write plaintext after this, so the sweep runs
// once more later. Both runs go away in a later release.
func (a *App) upgradePlaintextColumns() error {
	if _, err := a.Database.UpgradePlaintextColumns(context.Background()); err != nil {
		return err
	}
	time.AfterFunc(columnUpgradeRepeatDelay, func() {
		repeatColumnUpgrade(context.Background(), a.Database.UpgradePlaintextColumns, captureColumnUpgradeAlert)
	})
	return nil
}

// repeatColumnUpgrade runs the sweep again and reports it encrypting anything. By now this release, which never
// stores plaintext, has replaced the previous one, so a plaintext secret was written straight into the database
// (a known TOTP seed planted for a user, say) unless this is the rollout to column encryption and the previous
// release stored it meanwhile. The sweep has logged each row it encrypted.
func repeatColumnUpgrade(
	ctx context.Context,
	upgrade func(context.Context) (map[string]db.ColumnUpgradeCounts, error),
	report func(message string),
) {
	upgraded, err := upgrade(ctx)
	if err != nil {
		gtwlog.Errorf("Column encryption: repeat upgrade failed: %v", err)
		return
	}
	encrypted := make([]string, 0, len(upgraded))
	for column, counts := range upgraded {
		if counts.Encrypted > 0 {
			encrypted = append(encrypted, fmt.Sprintf("%s: %d", column, counts.Encrypted))
		}
	}
	if len(encrypted) == 0 {
		return
	}
	sort.Strings(encrypted)
	message := fmt.Sprintf("Column encryption: the sweep %s after startup encrypted plaintext secrets (%s). "+
		"Unless the previous release stored them during the upgrade to column encryption, someone wrote them "+
		"straight into the database; the warnings logged for each row name them",
		columnUpgradeRepeatDelay, strings.Join(encrypted, ", "))
	gtwlog.Errorf("%s", message)
	report(message)
}

func captureColumnUpgradeAlert(message string) {
	sentry.WithScope(func(scope *sentry.Scope) {
		scope.SetLevel(sentry.LevelError)
		scope.SetTag("component", "column_encryption")
		sentry.CaptureMessage(message)
	})
}
