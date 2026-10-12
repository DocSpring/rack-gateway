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
	upgraded, err := a.Database.UpgradePlaintextColumns(context.Background())
	if err != nil {
		return err
	}
	reportPlaintextInUpgradedColumns(upgraded, captureColumnUpgradeAlert)
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
	encrypted := encryptedColumns(upgraded, func(db.ColumnUpgradeCounts) bool { return true })
	if encrypted == "" {
		return
	}
	message := fmt.Sprintf("Column encryption: the sweep %s after startup encrypted plaintext secrets (%s). "+
		"Unless the previous release stored them during the upgrade to column encryption, someone wrote them "+
		"straight into the database; the warnings logged for each row name them",
		columnUpgradeRepeatDelay, encrypted)
	gtwlog.Errorf("%s", message)
	report(message)
}

// reportPlaintextInUpgradedColumns reports the startup sweep encrypting plaintext in a column that already held
// values encrypted under this key. That column was upgraded before, and this release and the one before it never
// store plaintext, so someone wrote it straight into the database. On the first upgrade every value is plaintext
// and nothing is reported.
func reportPlaintextInUpgradedColumns(upgraded map[string]db.ColumnUpgradeCounts, report func(message string)) {
	encrypted := encryptedColumns(upgraded, func(counts db.ColumnUpgradeCounts) bool { return counts.CurrentKey > 0 })
	if encrypted == "" {
		return
	}
	message := fmt.Sprintf("Column encryption: the startup sweep encrypted plaintext secrets in columns that "+
		"were already encrypted (%s), so someone wrote them straight into the database; the warnings logged for "+
		"each row name them", encrypted)
	gtwlog.Errorf("%s", message)
	report(message)
}

// encryptedColumns lists the columns include accepts where the sweep encrypted values, with how many, sorted and
// comma-separated, or "" for none.
func encryptedColumns(upgraded map[string]db.ColumnUpgradeCounts, include func(db.ColumnUpgradeCounts) bool) string {
	encrypted := make([]string, 0, len(upgraded))
	for column, counts := range upgraded {
		if counts.Encrypted > 0 && include(counts) {
			encrypted = append(encrypted, fmt.Sprintf("%s: %d", column, counts.Encrypted))
		}
	}
	sort.Strings(encrypted)
	return strings.Join(encrypted, ", ")
}

func captureColumnUpgradeAlert(message string) {
	sentry.WithScope(func(scope *sentry.Scope) {
		scope.SetLevel(sentry.LevelError)
		scope.SetTag("component", "column_encryption")
		sentry.CaptureMessage(message)
	})
}
