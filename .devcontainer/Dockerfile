# SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
# =============================================================================
# AF Selector dev container — base image
#
# We use the official Go image as the base (not the devcontainers/features
# one) because it already includes the toolchain, cgo headers, and git
# that we need. Node is added on top via the standard devcontainer
# `features` block in devcontainer.json.
#
# MySQL + Redis are NOT installed inside the dev container. We mount the
# host's Docker socket so the dev can `make dev-up` to start them as
# sibling containers using the existing docker-compose.yml. This way the
# dev DB is the same one the deploy uses, and the dev container stays
# small.
# =============================================================================
FROM golang:1.25-bookworm

# Common dev tooling. apt is fine in a dev container; prod images are
# different (see backend/Dockerfile for the slim prod image).
RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        git \
        make \
        ssh-client \
        tzdata \
        default-mysql-client \
        redis-tools \
    && rm -rf /var/lib/apt/lists/*

# Go modules cache lives in $GOPATH/pkg/mod by default; cache it on a
# named volume so `go mod download` is fast across container rebuilds.
ENV GOFLAGS="-mod=mod"
ENV GOPATH="/go"
ENV PATH="${GOPATH}/bin:${PATH}"

WORKDIR /workspaces/af
