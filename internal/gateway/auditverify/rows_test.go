package auditverify_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/auditverify"
	gwdb "github.com/DocSpring/rack-gateway/internal/gateway/db"
)

func TestVerifyPassesForAnUntouchedChain(t *testing.T) {
	database := newAuditDatabase(t)
	writeEvents(t, database, 5)

	report := verify(t, database, auditverify.Options{BatchSize: 2})
	requireOK(t, report)
	assert.Equal(t, int64(5), report.EventsChecked)
	assert.Equal(t, int64(5), report.V2Events)
	assert.Equal(t, int64(0), report.LegacyEvents)
	assert.True(t, report.Head.HashV2From.Valid)
	assert.Equal(t, int64(0), report.Head.HashV2From.Int64)

	var summary bytes.Buffer
	require.NoError(t, report.Write(&summary))
	assert.Contains(t, summary.String(), "Result: OK")
}

func TestVerifyDetectsEditedDetails(t *testing.T) {
	database := newAuditDatabase(t)
	writeEvents(t, database, 5)
	tamper(t, database, `UPDATE audit.audit_event SET details = '{"n":2}' WHERE chain_index = 2`)

	report := verify(t, database, auditverify.Options{})
	requireFailure(t, report, "event 2 ", "event_hash doesn't match")
	assert.Equal(t, 1, report.FailureCount)

	var summary bytes.Buffer
	require.NoError(t, report.Write(&summary))
	assert.Contains(t, summary.String(), "Result: FAILED")
}

func TestVerifyDetectsADeletedEvent(t *testing.T) {
	database := newAuditDatabase(t)
	writeEvents(t, database, 5)
	tamper(t, database, `DELETE FROM audit.audit_event WHERE chain_index = 2`)

	report := verify(t, database, auditverify.Options{})
	requireFailure(t, report, "event 3 ", "events 2..2 are missing")
	requireFailure(t, report, "event 3 ", "previous_hash doesn't match")
}

func TestVerifyDetectsAMissingGenesisEvent(t *testing.T) {
	database := newAuditDatabase(t)
	writeEvents(t, database, 3)
	tamper(t, database, `DELETE FROM audit.audit_event WHERE chain_index = 0`)

	report := verify(t, database, auditverify.Options{})
	requireFailure(t, report, "event 1 ", "doesn't start at the genesis event")
}

func TestVerifyDetectsReorderedEvents(t *testing.T) {
	database := newAuditDatabase(t)
	writeEvents(t, database, 5)
	tamper(t, database,
		`UPDATE audit.audit_event SET chain_index = 1000 WHERE chain_index = 1`,
		`UPDATE audit.audit_event SET chain_index = 1 WHERE chain_index = 2`,
		`UPDATE audit.audit_event SET chain_index = 2 WHERE chain_index = 1000`,
	)

	report := verify(t, database, auditverify.Options{})
	requireFailure(t, report, "event 1 ", "previous_hash doesn't match")
	requireFailure(t, report, "event 1 ", "event_hash doesn't match")
	requireFailure(t, report, "event 2 ", "event_hash doesn't match")
}

func TestVerifyDetectsCharactersShiftedBetweenFields(t *testing.T) {
	database := newAuditDatabase(t)
	writeEvents(t, database, 4)
	// "alice@example.com" + "Bob" and "alice@example.co" + "mBob" concatenate to the same bytes.
	tamper(t, database,
		`UPDATE audit.audit_event SET user_email = 'alice@example.co', user_name = 'mBob' WHERE chain_index = 3`)

	report := verify(t, database, auditverify.Options{})
	requireFailure(t, report, "event 3 ", "event_hash doesn't match")
}

func TestVerifyDetectsAV2EventRelabeledAsLegacy(t *testing.T) {
	database := newAuditDatabase(t)
	writeEvents(t, database, 4)
	tamper(t, database,
		`UPDATE audit.audit_event SET hash_version = 1, details = '{"n":9}' WHERE chain_index = 2`)

	report := verify(t, database, auditverify.Options{})
	requireFailure(t, report, "event 2 ", "marked as a legacy (v1) event after hash v2 started at event 0")
}

func TestVerifyChecksLegacyEventsByLinksAndReportsTheCutover(t *testing.T) {
	database := newAuditDatabase(t)
	// Legacy events can't have their HMAC recomputed. The third one skips index 2, as the old
	// sequence-based writer did after a failed insert.
	_, err := database.DB().Exec(`
		INSERT INTO audit.audit_event
			(chain_index, hash_version, previous_hash, event_hash, user_email, action_type, action, status)
		VALUES
			(0, 1, NULL, decode(repeat('01', 32), 'hex'), 'legacy@example.com', 'auth', 'login.start', 'success'),
			(1, 1, decode(repeat('01', 32), 'hex'), decode(repeat('02', 32), 'hex'),
				'legacy@example.com', 'auth', 'login.complete', 'success'),
			(3, 1, decode(repeat('02', 32), 'hex'), decode(repeat('03', 32), 'hex'),
				'legacy@example.com', 'convox', 'app.list', 'success')`)
	require.NoError(t, err)
	logs := writeEvents(t, database, 2)
	require.Equal(t, int64(4), logs[0].ChainIndex)
	pinned := auditverify.Options{HashV2From: int64Ptr(4)}

	report := verify(t, database, pinned)
	requireOK(t, report)
	assert.Equal(t, int64(3), report.LegacyEvents)
	assert.Equal(t, int64(2), report.V2Events)
	assert.Equal(t, int64(1), report.LegacyIndexGaps)
	assert.Equal(t, int64(4), report.Head.HashV2From.Int64)

	var summary bytes.Buffer
	require.NoError(t, report.Write(&summary))
	assert.Contains(t, summary.String(), "hash v2 from event 4")
	assert.Contains(t, summary.String(), "legacy (v1) events: 3 checked (up to event 3)")
	assert.Contains(t, summary.String(), "their content is unverifiable")
	// A passing run doesn't vouch for the legacy events' content.
	assert.Contains(t, summary.String(),
		"Result: OK, except that the content of 3 legacy (v1) events can't be verified")
	assert.Contains(t, summary.String(), "hash v2 start: pinned at event 4")

	// A broken link between legacy events is still caught.
	tamper(t, database, `UPDATE audit.audit_event SET previous_hash = decode(repeat('09', 32), 'hex')
		WHERE chain_index = 1`)
	requireFailure(t, verify(t, database, pinned), "event 1 ", "previous_hash doesn't match")
}

func TestVerifyChecksOnlyTheRequestedRange(t *testing.T) {
	database := newAuditDatabase(t)
	writeEvents(t, database, 6)
	tamper(t, database, `UPDATE audit.audit_event SET details = '{"n":2}' WHERE chain_index = 1`)

	report := verify(t, database, auditverify.Options{From: 3})
	requireOK(t, report)
	assert.Equal(t, int64(3), report.EventsChecked)
	assert.Equal(t, int64(3), report.FirstChecked)

	report = verify(t, database, auditverify.Options{From: 0, To: int64Ptr(2)})
	requireFailure(t, report, "event 1 ")
	assert.Equal(t, int64(3), report.EventsChecked)
}

func TestVerifyRequiresTheSecret(t *testing.T) {
	database := newAuditDatabase(t)
	_, err := auditverify.Run(context.Background(), database, auditverify.Options{})
	require.Error(t, err)

	t.Setenv("AUDIT_HMAC_SECRET", "")
	_, err = gwdb.AuditHMACSecretFromEnv()
	require.Error(t, err, "verify-audit must not fall back to the development secret")
}

func TestVerifyWithTheWrongSecretFails(t *testing.T) {
	database := newAuditDatabase(t)
	writeEvents(t, database, 2)

	report, err := auditverify.Run(context.Background(), database, auditverify.Options{Secret: []byte("wrong")})
	require.NoError(t, err)
	requireFailure(t, report, "event 0 ", "AUDIT_HMAC_SECRET is wrong")
}
