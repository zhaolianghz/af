# syntax=docker/dockerfile:1
# SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
#
# Pinned to Go 1.25 to match backend/go.mod. Bumping this without bumping
# go.mod (or vice versa) will break the build — there's a CI check for
# this drift in .github/workflows/ci.yml (version-check job).
FROM golang:1.25-alpine AS builder
WORKDIR /src

# Cache modules first
COPY go.mod go.sum* ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/af-backend ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
ENV TZ=Asia/Shanghai
WORKDIR /app
COPY --from=builder /out/af-backend /app/af-backend
COPY --from=builder /src/configs /app/configs

EXPOSE 8080
ENTRYPOINT ["/app/af-backend", "-config", "/app/configs/config.example.yaml"]
