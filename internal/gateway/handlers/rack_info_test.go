package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/DocSpring/rack-gateway/internal/gateway/testutil/dbtest"
)

func getRackInfo(t *testing.T, rack http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	server := httptest.NewServer(rack)
	t.Cleanup(server.Close)
	handler, _ := newAPIHandler(t, dbtest.NewDatabase(t), server.URL)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/rack", nil)
	handler.GetRackInfo(c)
	return w
}

// A rack error isn't passed on as the rack's information, even when its body is JSON.
func TestRackInfoReportsARackErrorAsAFailure(t *testing.T) {
	w := getRackInfo(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid authentication"}`))
	})
	require.Equal(t, http.StatusBadGateway, w.Code)
	require.JSONEq(t, `{"error":"the rack answered 401"}`, w.Body.String())
}

func TestRackInfoReturnsTheRacksSystem(t *testing.T) {
	w := getRackInfo(t, func(w http.ResponseWriter, r *http.Request) {
		user, key, ok := r.BasicAuth()
		if r.URL.Path != "/system" || !ok || user != "convox" || key != "token" {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"name":"test","version":"3.25.10"}`))
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.JSONEq(t, `{"name":"test","version":"3.25.10"}`, w.Body.String())
}
