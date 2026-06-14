#!/usr/bin/env bash
# Reproducible quakeforge -> Go transpile driver.
#
# Run inside a linux/arm64 (or linux/amd64) environment with:
#   - Go 1.23+ (in $PATH)
#   - modernc.org/ccgo/v4 installed (`go install modernc.org/ccgo/v4@latest`)
#   - autoconf / automake / libtool (for quakeforge ./bootstrap + ./configure)
#   - gcc + standard build-essential (so configure detects the host toolchain)
#
# Produces:
#   - <REPO>/engine/engine.go  - the transpiled pure-Go engine
#   - <REPO>/internal/transpile/sources.txt  - the exact .c file list ccgo saw
#   - <REPO>/internal/transpile/transpile.log  - stderr from ccgo (link gaps)
#
# Exit codes:
#   0  - engine.go produced; check transpile.log for undefined externs
#   1  - prerequisite missing
#   2  - quakeforge checkout / configure failed
#   3  - ccgo invocation failed catastrophically (parse error, not link)

set -euo pipefail

# Make standard Go install locations visible without relying on the caller's
# profile (non-interactive ssh sessions don't source ~/.profile etc.).
for d in /usr/local/go/bin "$HOME/go/bin" /root/go/bin; do
    [[ -d "$d" && ":$PATH:" != *":$d:"* ]] && PATH="$d:$PATH"
done
export PATH

REPO_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
QF_REF="${QF_REF:-master}"        # quakeforge git ref to transpile
WORK="${WORK:-/tmp/goquake1-transpile}"
QF_DIR="$WORK/quakeforge"
ENGINE_DIR="$REPO_DIR/engine"
TR_DIR="$REPO_DIR/internal/transpile"

mkdir -p "$WORK" "$ENGINE_DIR"

# 1) prereqs. quakeforge master requires C23 #embed; gcc-14 lacks it but
# clang-19 has it. Prefer an explicitly-set CC if the operator passed one,
# otherwise probe for a #embed-capable compiler.
if [[ -z "${CC:-}" ]]; then
    for try in clang-19 clang-20 gcc-15; do
        if command -v "$try" >/dev/null 2>&1; then
            CC="$try"
            break
        fi
    done
    if [[ -z "${CC:-}" ]]; then
        echo "no #embed-capable compiler found (need clang-19+, gcc-15+)" >&2
        echo "install: sudo apt-get install -y clang-19" >&2
        exit 1
    fi
fi
export CC
echo "[transpile] using CC=$CC" >&2

for cmd in go ccgo "$CC" autoconf automake libtool make git; do
    command -v "$cmd" >/dev/null 2>&1 \
        || { echo "missing prerequisite: $cmd" >&2; exit 1; }
done

# 2) quakeforge checkout
if [[ ! -d "$QF_DIR/.git" ]]; then
    echo "[transpile] cloning quakeforge@$QF_REF" >&2
    git clone --depth 1 --branch "$QF_REF" \
        https://github.com/quakeforge/quakeforge.git "$QF_DIR" >&2 \
        || { echo "quakeforge clone failed" >&2; exit 2; }
fi

# 3) bootstrap + configure (generates config.h that the engine sources #include)
if [[ ! -f "$QF_DIR/config.h" ]]; then
    echo "[transpile] bootstrap + configure" >&2
    ( cd "$QF_DIR" && ./bootstrap >&2 ) \
        || { echo "quakeforge bootstrap failed" >&2; exit 2; }
    # --with-clients=fbdev = framebuffer client only (closest analog to our
    # virtio-gpu scanout target; replaces the X11/SDL clients).
    # --with-servers="nq qw" = listenserver only (no dedicated host needed).
    #
    # NOTE on --disable-X flags: quakeforge's m4/quakeforge.m4 QF_REQUIRES
    # macro on *linux* unconditionally REQUIRES libflac-dev / libogg-dev /
    # libvorbis-dev / libxext-dev / libxxf86vm-dev / libvulkan-dev to be
    # present AND detected; passing --disable-flac skips the AC detection,
    # leaves HAVE_FLAC unset, then QF_REQUIRES fails the build. So we let
    # configure detect them all (the apt-get prerequisite list in the
    # Dockerfile installs them), and rely on skip.txt to keep the
    # corresponding .c files out of ccgo. The transpiled engine never sees
    # them.
    ( cd "$QF_DIR" && ./configure \
        --enable-static --disable-shared \
        --disable-asmopt \
        --with-clients=fbdev \
        --with-servers="nq qw" \
        --without-libcurl \
        >&2 ) \
        || { echo "quakeforge configure failed" >&2; exit 2; }
fi

# 4) candidate .c file list = every .c under nq/, libs/, ruamoko/ that is
#    NOT matched by skip.txt. Strip comments + blank lines from skip.txt
#    to a tempfile first -- an empty pattern in `grep -vEf` matches every
#    line and would silently exclude the entire candidate set.
SKIP_TMP="$WORK/skip.clean"
grep -vE '^\s*(#|$)' "$TR_DIR/skip.txt" > "$SKIP_TMP"
echo "[transpile] enumerating candidate .c files" >&2
(
    cd "$QF_DIR"
    find nq libs ruamoko -name '*.c' -type f 2>/dev/null
) | grep -vEf "$SKIP_TMP" | sort > "$TR_DIR/sources.txt"
N=$(wc -l < "$TR_DIR/sources.txt")
echo "[transpile] feeding ccgo $N .c files" >&2

# 5) Pin go.mod state in the engine directory (ccgo emits Go that needs libc).
if [[ ! -f "$ENGINE_DIR/go.mod" ]]; then
    ( cd "$ENGINE_DIR" && go mod init github.com/cloud-boot/goquake1/engine >&2 )
fi
( cd "$ENGINE_DIR" && go get modernc.org/libc >&2 ) || true

# 6) Run ccgo. Stderr captured for the post-mortem.
echo "[transpile] running ccgo" >&2
INCLUDES=(
    "-I$QF_DIR"
    "-I$QF_DIR/include"
    "-I$QF_DIR/include/QF"
)
DEFS=(
    "-DHAVE_CONFIG_H=1"
    "-DLINUX=1"
)
TRANSPILE_LOG="$TR_DIR/transpile.log"

# ccgo wants paths relative to its CWD; cd to quakeforge root so sources.txt
# entries (which are relative to that root) resolve.
(
    cd "$QF_DIR"
    set +e
    ccgo "${INCLUDES[@]}" "${DEFS[@]}" \
        -o "$ENGINE_DIR/engine.go" \
        $(cat "$TR_DIR/sources.txt") \
        2>"$TRANSPILE_LOG"
    rc=$?
    set -e

    # ccgo exit codes:
    #   - 0: transpile + link OK; engine.go ready to build
    #   - 1: usually link-time `undefined: X external` -- engine.go still
    #        produced as engine.o.go intermediates (or partial); these are
    #        the actionable TODOs.
    #   - other: parse error, structural failure.
    if [[ $rc -eq 0 ]]; then
        echo "[transpile] ccgo OK -- engine.go ready" >&2
    elif [[ $rc -eq 1 ]] && grep -q "undefined" "$TRANSPILE_LOG"; then
        UNDEFS=$(grep -c "undefined:" "$TRANSPILE_LOG" || true)
        echo "[transpile] ccgo link gaps: $UNDEFS undefined externs (see $TRANSPILE_LOG)" >&2
        echo "[transpile]   resolve by adding the missing .c files to sources or" >&2
        echo "[transpile]   providing tamago-side stubs in backend/tamago/cshim/" >&2
    else
        echo "[transpile] ccgo failed catastrophically (rc=$rc)" >&2
        tail -20 "$TRANSPILE_LOG" >&2
        exit 3
    fi
)

# 7) Summarise
echo "[transpile] done" >&2
ls -la "$ENGINE_DIR"/*.go 2>/dev/null | head -10 >&2 || true
