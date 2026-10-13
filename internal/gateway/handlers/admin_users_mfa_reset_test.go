package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/audit"
	"github.com/DocSpring/rack-gateway/internal/gateway/auth"
	"github.com/DocSpring/rack-gateway/internal/gateway/config"
	"github.com/DocSpring/rack-gateway/internal/gateway/db"
	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

type sentEmail struct{ to, subject string }

type recordingEmailSender struct{ sent []sentEmail }

func (r *recordingEmailSender) Send(to, subject, _, _ string) error {
	r.sent = append(r.sent, sentEmail{to: to, subject: subject})
	return nil
}

func (r *recordingEmailSender) SendMany(to []string, subject, textBody, htmlBody string) error {
	for _, recipient := range to {
		_ = r.Send(recipient, subject, textBody, htmlBody)
	}
	return nil
}

func resetUserMFA(t *testing.T, h *AdminHandler, admin *db.User, email string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/"+email+"/mfa/reset", nil)
	c.Request = req.WithContext(context.WithValue(req.Context(), auth.UserContextKey, &auth.User{
		Email: admin.Email, Name: admin.Name, Roles: admin.Roles, DBUser: admin,
	}))
	c.Params = gin.Params{{Key: "email", Value: email}}
	c.Set("user_email", admin.Email)
	h.ResetUserMFA(c)
	return w
}

// An admin's Reset MFA removes everything the user signs in with but keeps their API tokens.
func TestResetUserMFA(t *testing.T) {
	database := dbtest.NewDatabase(t)
	admin, err := database.CreateUser("admin@example.com", "Admin", []string{"admin"})
	require.NoError(t, err)
	user, err := database.CreateUser("dev@example.com", "Dev", []string{"deployer"})
	require.NoError(t, err)

	method, err := database.CreateMFAMethod(user.ID, "totp", "Phone", "JBSWY3DPEHPK3PXP", nil, nil, nil, nil)
	require.NoError(t, err)
	require.NoError(t, database.ConfirmMFAMethod(method.ID, time.Now()))
	require.NoError(t, database.SetUserMFAEnrolled(user.ID, true))
	require.NoError(t, database.ReplaceBackupCodes(user.ID, []string{strings.Repeat("1", 64)}))
	deviceID := "3f2b9c1e-8d4a-4b6e-9f1a-2c7d5e8a1b0f"
	_, err = database.CreateTrustedDevice(user.ID, deviceID, strings.Repeat("d", 64), time.Now().Add(time.Hour),
		"127.0.0.1", "ua", nil)
	require.NoError(t, err)
	_, err = database.CreateUserSession(user.ID, strings.Repeat("e", 64), time.Now().Add(time.Hour), "web",
		deviceID, "Laptop", "127.0.0.1", "ua", nil, nil)
	require.NoError(t, err)
	_, err = database.CreateAPIToken(strings.Repeat("f", 64), "dev-ci", user.ID, []string{"deployer"}, nil, nil)
	require.NoError(t, err)

	emails := &recordingEmailSender{}
	h := &AdminHandler{
		database:    database,
		auditLogger: audit.NewLogger(database),
		config:      &config.Config{},
		emailSender: emails,
	}

	w := resetUserMFA(t, h, admin, admin.Email)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "you can't reset your own MFA")

	w = resetUserMFA(t, h, admin, user.Email)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	methods, err := database.ListAllMFAMethods(user.ID)
	require.NoError(t, err)
	require.Empty(t, methods)
	codes, err := database.ListBackupCodes(user.ID)
	require.NoError(t, err)
	require.Empty(t, codes)
	devices, err := database.ListTrustedDevices(user.ID)
	require.NoError(t, err)
	for _, device := range devices {
		require.NotNil(t, device.RevokedAt)
		require.Equal(t, "admin_mfa_reset", device.RevokedReason)
	}
	sessions, err := database.ListActiveSessionsByUser(user.ID)
	require.NoError(t, err)
	require.Empty(t, sessions)
	reloaded, err := database.GetUserByID(user.ID)
	require.NoError(t, err)
	require.False(t, reloaded.MFAEnrolled)
	tokens, err := database.ListAPITokensByUser(user.ID)
	require.NoError(t, err)
	require.Len(t, tokens, 1)

	require.Equal(t, []sentEmail{{to: user.Email, subject: "Multi-factor authentication reset"}}, emails.sent)
	logs, err := database.GetAuditLogs(admin.Email, time.Time{}, 10)
	require.NoError(t, err)
	require.NotEmpty(t, logs)
	require.Equal(t, "user.mfa_reset", logs[0].Action)
	require.Equal(t, audit.StatusSuccess, logs[0].Status)
}
