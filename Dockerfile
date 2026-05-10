FROM golang:1.25-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN ls -la cmd/ && ls -la cmd/proxy/

RUN CGO_ENABLED=0 go build -o /proxy ./cmd/proxy
RUN CGO_ENABLED=0 go build -o /server ./cmd/server

# Server image
FROM alpine:3.19 AS server
WORKDIR /app
COPY --from=builder /server .
COPY config.json .
EXPOSE 8080
CMD ["./server", "-config", "config.json"]

# Proxy image
FROM alpine:3.19 AS proxy
WORKDIR /app
COPY --from=builder /proxy .
COPY config.json .
EXPOSE 6543
CMD ["./proxy", "-config", "config.json"]
