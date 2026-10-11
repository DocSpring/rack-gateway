package db_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gwdb "github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

const (
	testAuditHMACKey = "test-audit-hmac-secret"
	paddedCIName     = "  ci token  " // stored trimmed
)

func newAuditTestDatabase(t *testing.T) *gwdb.Database {
	t.Helper()
	t.Setenv("AUDIT_HMAC_SECRET", testAuditHMACKey)
	return dbtest.NewDatabase(t)
}

// requireStoredHashMatches reads the event back and recomputes its hash from the stored row.
func requireStoredHashMatches(t *testing.T, database *gwdb.Database, index int64) *gwdb.AuditChainRow {
	t.Helper()
	row, err := database.AuditChainRowAt(context.Background(), index)
	require.NoError(t, err)
	require.NotNil(t, row, "event %d should exist", index)
	assert.Equal(t, gwdb.AuditHashVersion, row.HashVersion)
	expected, err := gwdb.ComputeAuditEventHash([]byte(testAuditHMACKey), row)
	require.NoError(t, err)
	assert.Equal(t, expected, row.EventHash, "the stored event must hash to its stored event_hash")
	return row
}

func TestCreateAuditLogHashMatchesStoredRow(t *testing.T) {
	database := newAuditTestDatabase(t)
	tokenID := int64(7)
	approvalID := int64(42)
	log := &gwdb.AuditLog{
		// Nanoseconds and a non-UTC zone: Postgres keeps microseconds.
		Timestamp:               time.Date(2026, 3, 4, 5, 6, 7, 123456789, time.FixedZone("UTC+13", 13*3600)),
		UserEmail:               "alice@example.com",
		UserName:                "Alice",
		APITokenID:              &tokenID,
		APITokenName:            paddedCIName,
		ActionType:              "convox",
		Action:                  "app.list",
		Details:                 `{"request_id":"req-123","app":"web"}`,
		IPAddress:               "::ffff:10.1.2.3",
		UserAgent:               "test-agent",
		Status:                  "success",
		RBACDecision:            "allow",
		HTTPStatus:              200,
		ResponseTimeMs:          12,
		DeployApprovalRequestID: &approvalID,
	}
	require.NoError(t, database.CreateAuditLog(log))

	row := requireStoredHashMatches(t, database, log.ChainIndex)
	assert.Equal(t, 123456000, row.Timestamp.Nanosecond())
	assert.True(t, row.Timestamp.Equal(log.Timestamp), "the caller sees the stored timestamp")
	assert.Equal(t, log.ID, row.ID)
	assert.Equal(t, log.EventHash, row.EventHash)
	assert.Equal(t, sql.NullString{String: "req-123", Valid: true}, row.RequestID)
	assert.Equal(t, sql.NullString{String: `{"app":"web"}`, Valid: true}, row.Details)
	assert.Equal(t, sql.NullString{String: "ci token", Valid: true}, row.APITokenName)
	assert.Equal(t, sql.NullString{String: "::ffff:10.1.2.3", Valid: true}, row.IPAddress)
	assert.Equal(t, sql.NullInt64{Int64: 42, Valid: true}, row.DeployApprovalRequestID)
	assert.Nil(t, row.PreviousHash, "the first event is the genesis event")

	next := &gwdb.AuditLog{UserEmail: "bob@example.com", ActionType: "auth", Action: "login.start", Status: "success"}
	require.NoError(t, database.CreateAuditLog(next))
	nextRow := requireStoredHashMatches(t, database, next.ChainIndex)
	assert.Equal(t, log.ChainIndex+1, nextRow.ChainIndex)
	assert.Equal(t, log.EventHash, nextRow.PreviousHash)
	assert.Equal(t, sql.NullString{}, nextRow.IPAddress)
	assert.Equal(t, sql.NullString{}, nextRow.APITokenName)
}

func TestCreateAuditLogTruncatesOversizedFields(t *testing.T) {
	database := newAuditTestDatabase(t)
	details := `{"blob":"` + strings.Repeat("x", 9000) + `"}`
	log := &gwdb.AuditLog{
		UserEmail:  "alice@example.com",
		UserName:   strings.Repeat("n", 300),
		ActionType: "convox",
		Action:     strings.Repeat("a", 300),
		Command:    strings.Repeat("c", 3000),
		Resource:   "bad\x00byte\xff",
		Details:    details,
		UserAgent:  strings.Repeat("u", 600),
		Status:     "success",
	}
	require.NoError(t, database.CreateAuditLog(log), "oversized values must not make the insert fail")

	assert.Equal(t, 8000, utf8.RuneCountInString(log.Details))
	assert.True(t, strings.HasSuffix(log.Details, "…[truncated from 9011 characters]"))
	assert.Equal(t, 2000, utf8.RuneCountInString(log.Command))
	assert.Equal(t, 150, utf8.RuneCountInString(log.Action))
	assert.Equal(t, 200, utf8.RuneCountInString(log.UserName))
	assert.Equal(t, 512, utf8.RuneCountInString(log.UserAgent))
	assert.Equal(t, "bad\uFFFDbyte\uFFFD", log.Resource)

	row := requireStoredHashMatches(t, database, log.ChainIndex)
	assert.Equal(t, log.Details, row.Details.String)

	next := &gwdb.AuditLog{UserEmail: "alice@example.com", ActionType: "convox", Action: "app.list", Status: "success"}
	require.NoError(t, database.CreateAuditLog(next))
	assert.Equal(t, log.ChainIndex+1, next.ChainIndex, "the chain stays contiguous")
	requireStoredHashMatches(t, database, next.ChainIndex)
}

func TestAppendAuditEventRefusesBrokenLinks(t *testing.T) {
	database := newAuditTestDatabase(t)
	ctx := context.Background()
	first := &gwdb.AuditLog{
		UserEmail: "alice@example.com", ActionType: "auth", Action: "login.start", Status: "success",
	}
	require.NoError(t, database.CreateAuditLog(first))

	newRow := func(index int64, previousHash []byte) *gwdb.AuditChainRow {
		row := gwdb.NewAuditChainRowForTest(&gwdb.AuditLog{
			UserEmail: "mallory@example.com", ActionType: "auth", Action: "login.start", Status: "success",
		})
		row.ID = 1000 + index
		row.ChainIndex = index
		row.PreviousHash = previousHash
		return row
	}

	err := database.InsertAuditChainRowForTest(ctx, newRow(5, first.EventHash), []byte(testAuditHMACKey))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not follow the latest index")

	err = database.InsertAuditChainRowForTest(ctx, newRow(1, []byte("not the latest hash")), []byte(testAuditHMACKey))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "previous_hash does not match")

	require.NoError(t, database.InsertAuditChainRowForTest(ctx, newRow(1, first.EventHash), []byte(testAuditHMACKey)))
}

func TestComputeAuditEventHashSeparatesFields(t *testing.T) {
	secret := []byte(testAuditHMACKey)
	base := gwdb.AuditChainRow{
		ID:          1,
		HashVersion: gwdb.AuditHashVersion,
		Timestamp:   time.Date(2026, 1, 2, 3, 4, 5, 6000, time.UTC),
		EventHash:   []byte("ignored"),
		UserEmail:   "alice@example.com",
		UserName:    sql.NullString{String: "Bob", Valid: true},
		ActionType:  "convox",
		Action:      "app.list",
		Status:      "success",
	}
	hash := func(row gwdb.AuditChainRow) []byte {
		t.Helper()
		sum, err := gwdb.ComputeAuditEventHash(secret, &row)
		require.NoError(t, err)
		return sum
	}
	baseHash := hash(base)

	shifted := base
	shifted.UserEmail = "alice@example.co"
	shifted.UserName = sql.NullString{String: "mBob", Valid: true}
	assert.NotEqual(t, baseHash, hash(shifted), "moving characters between fields must change the hash")

	nullName := base
	nullName.UserName = sql.NullString{}
	emptyName := base
	emptyName.UserName = sql.NullString{String: "", Valid: true}
	assert.NotEqual(t, hash(nullName), hash(emptyName), "NULL and empty must hash differently")

	differentHash := base
	differentHash.EventHash = []byte("other")
	assert.Equal(t, baseHash, hash(differentHash), "event_hash isn't part of its own input")

	legacy := base
	legacy.HashVersion = gwdb.AuditHashVersionLegacy
	_, err := gwdb.ComputeAuditEventHash(secret, &legacy)
	require.Error(t, err)
	_, err = gwdb.ComputeAuditEventHash(nil, &base)
	require.Error(t, err)
}

func TestAuditHMACSecretFromEnv(t *testing.T) {
	t.Setenv("AUDIT_HMAC_SECRET", "")
	_, err := gwdb.AuditHMACSecretFromEnv()
	require.Error(t, err, "verification must never fall back to the development secret")

	t.Setenv("AUDIT_HMAC_SECRET", "   ")
	_, err = gwdb.AuditHMACSecretFromEnv()
	require.Error(t, err)

	t.Setenv("AUDIT_HMAC_SECRET", "real-secret")
	secret, err := gwdb.AuditHMACSecretFromEnv()
	require.NoError(t, err)
	assert.Equal(t, []byte("real-secret"), secret)
}
