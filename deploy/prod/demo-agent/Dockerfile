FROM golang:1.25-alpine AS builder

WORKDIR /src
COPY go.mod ./
COPY . ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /bin/demo-agent .

FROM gcr.io/distroless/static-debian12:nonroot

USER nonroot:nonroot
WORKDIR /
COPY --from=builder /bin/demo-agent /bin/demo-agent

ENTRYPOINT ["/bin/demo-agent"]
