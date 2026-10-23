package db_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gwdb "github.com/DocSpring/rack-gateway/internal/gateway/db"
)

func newTrustedDevice(t *testing.T, database *gwdb.Database, userID int64, n int) *gwdb.TrustedDevice {
	t.Helper()
	device, err := database.CreateTrustedDevice(userID, fmt.Sprintf("00000000-0000-0000-0000-%012d", n),
		fmt.Sprintf("%064d", n),
		time.Now().Add(time.Hour), "127.0.0.1", "agent", nil)
	require.NoError(t, err)
	return device
}

func newConfirmedMethod(t *testing.T, database *gwdb.Database, userID int64) *gwdb.MFAMethod {
	t.Helper()
	method, err := database.CreateMFAMethod(userID, "totp", "TOTP", "secret", nil, nil, nil, nil)
	require.NoError(t, err)
	require.NoError(t, database.FinalizeMFAEnrollment(userID, method.ID, time.Now(), nil))
	return method
}

func requireDeviceRevoked(t *testing.T, database *gwdb.Database, deviceID int64, revoked bool) {
	t.Helper()
	device, err := database.GetTrustedDeviceByID(deviceID)
	require.NoError(t, err)
	require.Equal(t, revoked, device.RevokedAt != nil, "trusted device %d revoked", deviceID)
}

func requireEnrolled(t *testing.T, database *gwdb.Database, userID int64, enrolled bool) {
	t.Helper()
	user, err := database.GetUserByID(userID)
	require.NoError(t, err)
	require.Equal(t, enrolled, user.MFAEnrolled)
}

// A device trusted before the user had a factor verified nothing, so the first enrollment revokes it. Later
// enrollments keep the user's trusted devices.
func TestFirstEnrollmentRevokesTrustedDevices(t *testing.T) {
	database, user := newEnrollmentUser(t)
	before := newTrustedDevice(t, database, user.ID, 1)
	newConfirmedMethod(t, database, user.ID)
	after := newTrustedDevice(t, database, user.ID, 2)
	newConfirmedMethod(t, database, user.ID)

	requireDeviceRevoked(t, database, before.ID, true)
	requireDeviceRevoked(t, database, after.ID, false)
}

// Deleting the user's last confirmed method marks them not enrolled and revokes their trusted devices in the same
// transaction; deleting another leaves both alone.
func TestDeleteUserMFAMethodReconcilesEnrollment(t *testing.T) {
	database, user := newEnrollmentUser(t)
	first := newConfirmedMethod(t, database, user.ID)
	second := newConfirmedMethod(t, database, user.ID)
	device := newTrustedDevice(t, database, user.ID, 1)

	other, err := database.CreateUser("other@example.com", "Other", []string{"viewer"})
	require.NoError(t, err)
	require.ErrorIs(t, database.DeleteUserMFAMethod(other.ID, first.ID), gwdb.ErrMFAMethodNotFound)

	require.NoError(t, database.DeleteUserMFAMethod(user.ID, first.ID))
	requireEnrolled(t, database, user.ID, true)
	requireDeviceRevoked(t, database, device.ID, false)

	require.NoError(t, database.DeleteUserMFAMethod(user.ID, second.ID))
	requireEnrolled(t, database, user.ID, false)
	requireDeviceRevoked(t, database, device.ID, true)
	require.ErrorIs(t, database.DeleteUserMFAMethod(user.ID, second.ID), gwdb.ErrMFAMethodNotFound)
}

// Backup codes an enrollment shows verify nothing until its factor is confirmed. Codes are issued for the enrollment
// the request was authorized for, and an enrolled user's codes are kept.
func TestEnrollmentBackupCodesVerifyOnlyOnceEnrolled(t *testing.T) {
	database, user := newEnrollmentUser(t)
	issued, err := database.IssueEnrollmentBackupCodes(user.ID, false, []string{"hash-1", "hash-2"})
	require.NoError(t, err)
	require.True(t, issued)
	used, err := database.MarkBackupCodeUsed(user.ID, "hash-1")
	require.NoError(t, err)
	require.False(t, used, "a backup code verified before the user confirmed a factor")

	_, err = database.IssueEnrollmentBackupCodes(user.ID, true, []string{"hash-3"})
	require.ErrorIs(t, err, gwdb.ErrMFAEnrollmentChanged)

	newConfirmedMethod(t, database, user.ID)
	_, err = database.IssueEnrollmentBackupCodes(user.ID, false, []string{"hash-3"})
	require.ErrorIs(t, err, gwdb.ErrMFAEnrollmentChanged)
	issued, err = database.IssueEnrollmentBackupCodes(user.ID, true, []string{"hash-3"})
	require.NoError(t, err)
	require.False(t, issued, "an enrolled user's backup codes were replaced")

	used, err = database.MarkBackupCodeUsed(user.ID, "hash-1")
	require.NoError(t, err)
	require.True(t, used)
	used, err = database.MarkBackupCodeUsed(user.ID, "hash-1")
	require.NoError(t, err)
	require.False(t, used, "a used backup code verified again")
}
