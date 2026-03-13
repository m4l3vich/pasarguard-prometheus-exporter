# Build stage
FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /exporter ./cmd/exporter/

# Runtime stage
FROM gcr.io/distroless/static:nonroot
COPY --from=builder /exporter /exporter
USER nonroot:nonroot
EXPOSE 9115
ENTRYPOINT ["/exporter"]
