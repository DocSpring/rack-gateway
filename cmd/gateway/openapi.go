// Package main contains the Rack Gateway HTTP server.
//
// @title Rack Gateway API
// @version 1.0
// @description API for the Rack Gateway administration and proxy services.
// @BasePath /.gateway/api
// @schemes http https
// @securityDefinitions.apiKey SessionCookie
// @in header
// @name Cookie
// @description HttpOnly session cookie issued after OAuth login.
// @securityDefinitions.apiKey CSRFToken
// @in header
// @name X-CSRF-Token
// @description HMAC-derived CSRF token tied to the active session.
package main

func init() {}
