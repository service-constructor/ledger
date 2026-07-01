#
# Self-contained module — build context is ledger/ itself.
#
#   docker build -f ledger/Dockerfile -t serviceconstructor-ledger:latest ledger/
#
FROM golang:1.25-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/ledger ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ledger /ledger
# gRPC only, :9100 (see internal/config/config.go default).
EXPOSE 9100
USER nonroot:nonroot
ENTRYPOINT ["/ledger"]
