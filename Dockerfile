# syntax=docker/dockerfile:1.7

ARG GO_VERSION=1.25.9

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-bookworm AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH
ENV CGO_ENABLED=0

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -ldflags="-s -w" -o /out/pluggercat ./cmd/pluggercat

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /out/pluggercat /usr/local/bin/pluggercat

ENTRYPOINT ["/usr/local/bin/pluggercat"]
