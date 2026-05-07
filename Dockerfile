FROM golang:1.26-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /tamci ./cmd/tamci

FROM alpine:3.23

RUN apk add --no-cache ca-certificates git

COPY --from=builder /tamci /tamci

ENTRYPOINT ["/tamci"]
