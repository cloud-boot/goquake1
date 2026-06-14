# Reproducible transpile image for the cloud-boot/goquake1 pipeline.
#
# Why a container at all? quakeforge's `./configure` probes the host for
# audio/video/asm headers even when those backends are `--disable`d, so
# the operator needs a fixed set of dev packages installed up front
# (~25 of them) to get past configure. Pinning them in this image
# guarantees CI and ad-hoc Tart runs see the same toolchain.
#
# Why linux/arm64? The maintainer's workstation is Apple Silicon; ccgo
# refuses to parse Darwin's NEON struct alignment in <arm/_structs.h>
# (host transpile), and Docker-on-Apple-Silicon emulation of linux/amd64
# triggers gcc's missing `-m64` flag + Go runtime crashes (see
# memory: feedback-debian-test-containers). A native linux/arm64 VM or
# container is the only reliable path. Adjust the FROM line for an
# x86_64 host.
#
# Build:   docker build -f internal/transpile/Dockerfile -t goquake1-transpile .
# Or use Tart directly: this image is mirrored manually into the
#   `goquake-build` Tart VM by `internal/transpile/run.sh`.

FROM debian:13-slim

ENV DEBIAN_FRONTEND=noninteractive
ENV LANG=C.UTF-8

# Base toolchain + clang-19 (provides C23 #embed that quakeforge master needs)
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates curl git build-essential pkg-config \
    autoconf automake libtool libtool-bin flex bison gawk \
    clang-19 rsync \
    && rm -rf /var/lib/apt/lists/*

# quakeforge configure requires these dev headers to exist on disk even
# when their backend is --disable'd. Keep this list in sync with the
# error message produced by a vanilla `./configure --disable-X`.
RUN apt-get update && apt-get install -y --no-install-recommends \
    libasound2-dev libjack-jackd2-dev libsamplerate0-dev \
    libflac-dev libogg-dev libvorbis-dev \
    libxext-dev libxxf86vm-dev libxxf86dga-dev libxi-dev libxfixes-dev \
    libvulkan-dev \
    libfontconfig-dev libharfbuzz-dev libfreetype-dev \
    libpng-dev zlib1g-dev libcurl4-openssl-dev \
    && rm -rf /var/lib/apt/lists/*

# Go (latest stable; ccgo is published as a Go module).
RUN curl -fsSL https://go.dev/dl/go1.23.4.linux-arm64.tar.gz \
    | tar -C /usr/local -xz
ENV PATH=/usr/local/go/bin:/root/go/bin:$PATH

# ccgo: the C-to-Go transpiler that drives the engine translation.
RUN go install modernc.org/ccgo/v4@latest

# Default to clang-19 for quakeforge ./configure + make.
ENV CC=clang-19

WORKDIR /work

# Smoke check on image build: ccgo on hello world (~38 lines Go).
RUN mkdir -p /tmp/smoke && cd /tmp/smoke \
    && printf '#include <stdio.h>\nint main() { puts("ccgo smoke ok"); return 0; }\n' > hello.c \
    && go mod init smoke >/dev/null 2>&1 \
    && go get modernc.org/libc >/dev/null 2>&1 \
    && ccgo -o hello.go hello.c \
    && rm hello.c \
    && go build -o /tmp/smoke/hello . \
    && /tmp/smoke/hello \
    && rm -rf /tmp/smoke

CMD ["/bin/bash"]
