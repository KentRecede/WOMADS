# One Dockerfile builds either Go service:
#   docker build --build-arg SERVICE=api     -t womads-api .
#   docker build --build-arg SERVICE=gateway -t womads-gateway .
# (docker compose does this for you.)

FROM golang:1.23-alpine AS build
WORKDIR /src
# go.sum is created by `go mod tidy`; commit it, the build needs it.
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG SERVICE
RUN test -n "$SERVICE" && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/app ./cmd/${SERVICE}

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 app
USER app
COPY --from=build /out/app /app
ENTRYPOINT ["/app"]
