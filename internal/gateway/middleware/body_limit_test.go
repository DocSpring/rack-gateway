package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func bodyLimitRouter(limit int64, read *string, readErr *error) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(LimitRequestBody(limit))
	router.POST("/", func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		*read, *readErr = string(body), err
		c.Status(http.StatusNoContent)
	})
	return router
}

func TestLimitRequestBodyPassesABodyWithinTheLimit(t *testing.T) {
	var read string
	var readErr error
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("12345678"))
	bodyLimitRouter(8, &read, &readErr).ServeHTTP(rr, req)

	require.Equal(t, http.StatusNoContent, rr.Code)
	require.NoError(t, readErr)
	require.Equal(t, "12345678", read)
}

func TestLimitRequestBodyRefusesALargerContentLength(t *testing.T) {
	read, readErr := "untouched", error(nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("123456789"))
	bodyLimitRouter(8, &read, &readErr).ServeHTTP(rr, req)

	require.Equal(t, http.StatusRequestEntityTooLarge, rr.Code)
	require.Equal(t, "untouched", read, "the handler must not run")
}

// A body without a Content-Length (chunked) can't be refused up front, but reading past the limit fails.
func TestLimitRequestBodyStopsReadingAnUnknownLengthBody(t *testing.T) {
	var read string
	var readErr error
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("123456789"))
	req.ContentLength = -1
	rr := httptest.NewRecorder()
	bodyLimitRouter(8, &read, &readErr).ServeHTTP(rr, req)

	var tooLarge *http.MaxBytesError
	require.ErrorAs(t, readErr, &tooLarge)
	require.Len(t, read, 8)
}
