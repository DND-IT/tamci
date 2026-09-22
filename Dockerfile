FROM golang:1.26-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /tamci ./cmd/tamci

FROM alpine:3.23

RUN apk add --no-cache ca-certificates git curl

# Install git-cliff from GitHub releases (pinned version).
ARG GIT_CLIFF_VERSION=2.14.2
RUN ARCH=$(uname -m) && \
    case "$ARCH" in \
      x86_64)  ARCH="x86_64-unknown-linux-musl" ;; \
      aarch64) ARCH="aarch64-unknown-linux-musl" ;; \
    esac && \
    curl -sSfL "https://github.com/orhun/git-cliff/releases/download/v${GIT_CLIFF_VERSION}/git-cliff-${GIT_CLIFF_VERSION}-${ARCH}.tar.gz" \
      | tar xz -C /usr/local/bin git-cliff-${GIT_CLIFF_VERSION}/git-cliff --strip-components=1

COPY --from=builder /tamci /tamci
COPY --from=builder /app/cliff-templates /cliff-templates

ENTRYPOINT ["/tamci"]
