package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGatewayIdentityNamesTheRackAndPublicURL(t *testing.T) {
	cfg := &Config{
		Domain: "rack-gateway-staging.example.ts.net",
		Port:   "8447",
		Racks:  map[string]RackConfig{"default": {Name: "staging", DisplayName: " Staging "}},
	}

	gateway := cfg.GatewayIdentity()

	assert.Equal(t, GatewayIdentity{Name: "Staging", URL: "https://rack-gateway-staging.example.ts.net"}, gateway)
	assert.Equal(t, "Rack Gateway (Staging)", gateway.Label())
	assert.Equal(t, "Staging Rack Gateway", gateway.FullName())
}

func TestGatewayIdentityWithoutRackOrDomain(t *testing.T) {
	gateway := (&Config{}).GatewayIdentity()

	assert.Equal(t, GatewayIdentity{}, gateway)
	assert.Equal(t, "Rack Gateway", gateway.Label())
	assert.Equal(t, "Rack Gateway", gateway.FullName())
}

func TestNewGatewayIdentityTrimsNameAndURL(t *testing.T) {
	gateway := NewGatewayIdentity("  EU\n", " https://gw.example.com/ ")

	assert.Equal(t, GatewayIdentity{Name: "EU", URL: "https://gw.example.com"}, gateway)
}
