package cli

import (
	"io"
	"net/http"
)

func authorizedGatewayRequest(
	method string,
	endpoint string,
	sessionToken string,
	body io.Reader,
	contentType string,
) (*http.Response, error) {
	req, err := http.NewRequest(method, endpoint, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return HTTPClient.Do(req)
}
