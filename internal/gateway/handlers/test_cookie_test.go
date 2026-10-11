package handlers

import "net/http"

// testCookie is a request cookie for tests, with the attributes a gateway cookie has.
func testCookie(name, value string) *http.Cookie {
	return &http.Cookie{Name: name, Value: value, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode}
}
