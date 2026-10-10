package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
)

// The filter takes the state names job responses carry, so the Jobs page can filter by any state it shows.
func TestParseJobStateFilter(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, state := range rivertype.JobStates() {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		got, ok := parseJobStateFilter(c, string(state))
		assert.True(t, ok, state)
		assert.Equal(t, state, got)
	}

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	got, ok := parseJobStateFilter(c, "")
	assert.True(t, ok)
	assert.Empty(t, got)

	rec := httptest.NewRecorder()
	c, _ = gin.CreateTestContext(rec)
	_, ok = parseJobStateFilter(c, "finished")
	assert.False(t, ok)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
