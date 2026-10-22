package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/getsentry/sentry-go"
	sentrygin "github.com/getsentry/sentry-go/gin"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// A handler's panic is answered with 500 and sent to Sentry once, as a panic (not as a bare 500).
func TestRecoveryReportsPanicsToSentry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var mu sync.Mutex
	var events []*sentry.Event
	client, err := sentry.NewClient(sentry.ClientOptions{
		BeforeSend: func(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
			mu.Lock()
			defer mu.Unlock()
			events = append(events, event)
			return nil
		},
	})
	require.NoError(t, err)
	hub := sentry.CurrentHub()
	previous := hub.Client()
	hub.BindClient(client)
	t.Cleanup(func() { hub.BindClient(previous) })

	router := gin.New()
	router.Use(sentrygin.New(sentrygin.Options{Repanic: true}))
	router.Use(Recovery())
	router.Use(SentryErrorCapture())
	router.GET("/boom", func(c *gin.Context) {
		parts := strings.Split(c.Request.URL.Path, "/")
		index, _ := strconv.Atoi(c.Query("part"))
		c.String(http.StatusOK, parts[index]) // the request asks for a part that isn't there: the handler panics
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boom?part=9", nil))

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, events, 1)
	require.NotEmpty(t, events[0].Exception)
	require.Contains(t, events[0].Exception[0].Value, "index out of range")
}

func TestConnectionGone(t *testing.T) {
	require.True(t, connectionGone(http.ErrAbortHandler))
	require.True(t, connectionGone(fmt.Errorf("write: %w", syscall.EPIPE)))
	require.True(t, connectionGone(fmt.Errorf("read: %w", syscall.ECONNRESET)))
	require.False(t, connectionGone(fmt.Errorf("something else")))
	require.False(t, connectionGone("a string"))
}
