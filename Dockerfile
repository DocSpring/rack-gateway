FROM golang:1.23-alpine AS builder

RUN apk add --no-cache git ca-certificates make gcc musl-dev sqlite-dev

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=1 make gateway

FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

COPY --from=builder /app/bin/convox-gateway-api .
COPY --from=builder /app/config ./config
COPY --from=builder /app/web ./web

EXPOSE 8080

CMD ["./convox-gateway-api"]
