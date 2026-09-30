# Local tmux-version matrix image for the real-tmux race suite — replaces the
# deleted GitHub Actions tmux-e2e job. Built and run by
# scripts/ci_tmux_matrix.sh (`make ci-tmux-matrix`). Nothing is copied in: the
# repo is bind-mounted at /src at run time.
FROM ubuntu:22.04

ARG GO_VERSION
ARG GO_SHA256
ARG TMUX=apt
ARG TMUX_SHA256=
ARG TARGETARCH

ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates curl git build-essential \
      libevent-dev libncurses-dev bison pkg-config \
 && rm -rf /var/lib/apt/lists/*

# Same Go toolchain as go.mod's toolchain directive (passed as GO_VERSION),
# sha256-verified before extraction — the toolchain runs every -race gate in
# this image, so an unverified download would put unverified code inside the
# security boundary of the local CI gate. GO_SHA256 comes from the caller's
# go_sha() pin table.
RUN [ -n "$GO_VERSION" ] && [ -n "$TARGETARCH" ] && [ -n "$GO_SHA256" ] \
 && curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${TARGETARCH}.tar.gz" -o /tmp/go.tgz \
 && echo "${GO_SHA256}  /tmp/go.tgz" | sha256sum -c - \
 && tar -C /usr/local -xzf /tmp/go.tgz \
 && rm /tmp/go.tgz
ENV PATH=/usr/local/go/bin:$PATH \
    GOPATH=/go \
    GOCACHE=/cache/go-build \
    GOMODCACHE=/go/pkg/mod \
    LANG=C.UTF-8 \
    LC_ALL=C.UTF-8

# apt = ubuntu-22.04's stock tmux (3.2a, the distro floor). Anything else is a
# from-source build of a sha256-pinned release tarball (see the caller's
# tmux_sha — same pin table the old workflow used).
RUN set -e; \
    if [ "$TMUX" = "apt" ]; then \
      apt-get update \
      && apt-get install -y --no-install-recommends tmux \
      && rm -rf /var/lib/apt/lists/*; \
    else \
      [ -n "$TMUX_SHA256" ] || { echo "TMUX_SHA256 required for source builds" >&2; exit 1; }; \
      curl -fsSL "https://github.com/tmux/tmux/releases/download/${TMUX}/tmux-${TMUX}.tar.gz" -o /tmp/tmux.tar.gz \
      && echo "${TMUX_SHA256}  /tmp/tmux.tar.gz" | sha256sum -c - \
      && tar -xzf /tmp/tmux.tar.gz -C /tmp \
      && cd "/tmp/tmux-${TMUX}" && ./configure && make -j"$(nproc)" && make install && ldconfig; \
    fi; \
    tmux -V

# Tests run as an unprivileged user like the old CI runner (uid 1001): running
# as root would make permission-bit tests vacuous (root ignores search bits,
# so an "unsearchable" cwd never fails). Cache dirs are chowned so the named
# volumes stay writable.
RUN useradd -m -u 1001 ci \
 && mkdir -p /cache/go-build /go/pkg/mod \
 && chown -R ci:ci /cache /go
USER ci
ENV HOME=/home/ci

WORKDIR /src
