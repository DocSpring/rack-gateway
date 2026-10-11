package config

import "strings"

// GatewayIdentity names the gateway that sends a notification. One gateway runs per rack, and one person can
// get email, or one Slack workspace can get messages, from several gateways, so every email and Slack message
// says which gateway sent it.
type GatewayIdentity struct {
	// Name is the rack's display name (RACK_DISPLAY_NAME), e.g. "Staging" or "US".
	Name string
	// URL is the gateway's base URL without a trailing slash, e.g. https://rack-gateway-staging.example.ts.net.
	URL string
}

// NewGatewayIdentity returns the identity of the gateway called name at url, trimmed and without a trailing slash.
func NewGatewayIdentity(name, url string) GatewayIdentity {
	return GatewayIdentity{
		Name: strings.TrimSpace(name),
		URL:  strings.TrimRight(strings.TrimSpace(url), "/"),
	}
}

// GatewayIdentity returns this gateway's identity: the rack's display name and the gateway's public URL.
func (c *Config) GatewayIdentity() GatewayIdentity {
	return NewGatewayIdentity(c.Racks["default"].DisplayName, c.PublicURL())
}

// Label titles the gateway at the start of a notification: "Rack Gateway (Staging)", or "Rack Gateway" when it
// has no name.
func (g GatewayIdentity) Label() string {
	if g.Name == "" {
		return "Rack Gateway"
	}
	return "Rack Gateway (" + g.Name + ")"
}

// FullName names the gateway within a sentence: "Staging Rack Gateway", or "Rack Gateway" when it has no name.
func (g GatewayIdentity) FullName() string {
	if g.Name == "" {
		return "Rack Gateway"
	}
	return g.Name + " Rack Gateway"
}
