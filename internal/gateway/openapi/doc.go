package openapi

//go:generate sh -c "cd ../../../ && go run github.com/swaggo/swag/cmd/swag@v1.16.2 init -g cmd/gateway/main.go -o internal/gateway/openapi/generated --outputTypes json --parseDependency --parseInternal"
