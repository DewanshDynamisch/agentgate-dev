FROM golang:1.24-alpine AS builder

WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /support-desk-mcp .

FROM gcr.io/distroless/static-debian12:nonroot

USER nonroot:nonroot
WORKDIR /app
COPY --from=builder /support-desk-mcp /app/support-desk-mcp
EXPOSE 9200
ENTRYPOINT ["/app/support-desk-mcp"]
