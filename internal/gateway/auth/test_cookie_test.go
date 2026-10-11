package auth

import "net/http"

// sessionCookie is a session_token request cookie for tests, with the attributes the gateway's cookie has.
func sessionCookie(token string) *http.Cookie {
	return &http.Cookie{
		Name: "session_token", Value: token, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	}
}
