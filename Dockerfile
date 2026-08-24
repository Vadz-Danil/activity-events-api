FROM golang:1.27-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .

ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build \
      -trimpath \
      -ldflags="-s -w -X main.version=${VERSION}" \
      -o /out/app ./cmd/app
FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata wget \
 && adduser -D -H -u 10001 app

COPY --from=builder /out/app /usr/local/bin/app

USER app

EXPOSE 8080

ENTRYPOINT ["app"]
