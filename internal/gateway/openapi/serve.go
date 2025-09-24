package openapi

import (
	_ "embed"
	"net/http"

	"github.com/gin-gonic/gin"
)

//go:embed generated/swagger.json
var swaggerJSON []byte

const (
	defaultSpecPath        = "/.gateway/openapi.json"
	openapiJSONContentType = "application/json"
)

// Register adds the OpenAPI specification endpoint to the router.
func Register(router *gin.Engine) {
	router.GET(defaultSpecPath, func(c *gin.Context) {
		c.Data(http.StatusOK, openapiJSONContentType, swaggerJSON)
	})
}

// JSON returns the embedded OpenAPI specification.
func JSON() []byte {
	return swaggerJSON
}
