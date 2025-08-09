FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o convox-gateway-api cmd/api/main.go

FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

COPY --from=builder /app/convox-gateway-api .
COPY --from=builder /app/config ./config
COPY --from=builder /app/web ./web

EXPOSE 8080

CMD ["./convox-gateway-api"]