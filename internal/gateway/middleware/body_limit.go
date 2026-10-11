package middleware

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

// LimitRequestBody refuses a request body over limit bytes. One with a larger Content-Length gets 413 straight
// away, and reading past the limit fails, so a handler that reads the whole body (JSON binding does) can't be made
// to hold an arbitrarily large one in memory.
func LimitRequestBody(limit int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > limit {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{
				"error": fmt.Sprintf("request body too large: at most %d bytes", limit),
			})
			return
		}
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		}
		c.Next()
	}
}
