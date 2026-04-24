FROM golang:1.26.2-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o agent-proxy .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=builder /app/agent-proxy /usr/local/bin/agent-proxy
EXPOSE 7700 7701
ENTRYPOINT ["agent-proxy"]
