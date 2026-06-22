#!/usr/bin/env bash
# Copyright (c) 1996-1997 Id Software, Inc.
# Copyright (c) 2026 the go-quake1/engine authors.
# SPDX-License-Identifier: GPL-2.0-or-later
#
# capture_waypoints.sh boots the Quake-on-TamaGo ELF in QEMU headless,
# captures one PPM per demo-orbit waypoint via the QEMU monitor's
# `screendump` command, then kills QEMU. The demo-orbit cadence is
# sv.time-based (see demoWaypointPeriodSeconds in main.go); this
# harness tracks waypoint changes by tailing the serial log for the
# per-tic "think-census" line that carries sv.time, then triggers
# screendump as sv.time crosses each (period * waypoint_index + half)
# threshold so the capture lands mid-window (no risk of catching the
# transition tic).
#
# Output: /tmp/quake-orbit-NN.ppm (NN = 00 .. 03 for the 4-waypoint
# cycle the start.bsp lattice produces).
#
# Why poll sv.time rather than wall-clock: the virtio backend's
# Now() returns a *deterministic* 1/60 s tick (defaultClockStep) on
# every call, NOT wall-clock seconds -- so sv.time advances at
# (RunFrame iterations) / 60. On QEMU TCG without KVM the runloop
# sustains ~2 RunFrame/wall-clock-sec, which means sv.time crawls at
# ~0.033 s game-time per wall-clock-sec. A fixed 2 s sleep between
# screendumps would land all four captures inside waypoint[0]. The
# serial-log poll decouples the harness from that timing skew.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ELF="${SCRIPT_DIR}/build/quake.elf"
MON="/tmp/quake-orbit-mon.sock"
SERIAL="/tmp/quake-orbit-serial.log"
PPM_PREFIX="/tmp/quake-orbit"

# Waypoint period in sv.time seconds. MUST match
# demoWaypointPeriodSeconds in main.go -- if they drift the captures
# land off-centre in the waypoint windows.
PERIOD_SECONDS="${PERIOD_SECONDS:-2.0}"
NUM_WAYPOINTS="${NUM_WAYPOINTS:-4}"
# Maximum wall-clock seconds to wait for the engine to reach the
# final waypoint mid-window threshold. On QEMU TCG without KVM
# sv.time advances at ~0.033 s per wall-clock-sec (see header) so
# reaching sv.time = (NUM_WAYPOINTS - 0.5) * PERIOD_SECONDS = 7 s
# needs ~210 s wall-clock; bump to 600 s for safety margin + slower
# hosts. On a hardware build sv.time tracks wall-clock 1:1 + the
# captures complete in ~8 s.
MAX_WALL_SECS="${MAX_WALL_SECS:-600}"

QEMU="${QEMU:-qemu-system-x86_64}"
CPU="${CPU:-max}"
MEM="${MEM:-2G}"

if ! command -v "$QEMU" >/dev/null 2>&1; then
    echo "capture_waypoints.sh: $QEMU not found in PATH" >&2
    exit 1
fi
if [[ ! -s "$ELF" ]]; then
    echo "capture_waypoints.sh: $ELF missing or empty -- run 'task build' first" >&2
    exit 1
fi

rm -f "$MON" "$SERIAL"
for i in $(seq 0 $((NUM_WAYPOINTS - 1))); do
    printf -v idx '%02d' "$i"
    rm -f "${PPM_PREFIX}-${idx}.ppm"
done

# Launch QEMU headless in the background. The QEMU monitor binds to
# the unix socket; we drive screendump commands over it.
"$QEMU" -M q35 -accel tcg -cpu "$CPU" -m "$MEM" \
    -display none -no-reboot -vga none \
    -device virtio-gpu-pci,id=vgpu,xres=1280,yres=1024 \
    -device virtio-keyboard-pci,id=vkbd \
    -device virtio-mouse-pci,id=vmouse \
    -serial "file:${SERIAL}" \
    -monitor "unix:${MON},server,nowait" \
    -kernel "$ELF" &
QPID=$!

cleanup() {
    if kill -0 "$QPID" 2>/dev/null; then
        kill "$QPID" 2>/dev/null || true
        wait "$QPID" 2>/dev/null || true
    fi
    rm -f "$MON"
}
trap cleanup EXIT

# Wait for the monitor socket to appear.
for _ in $(seq 1 50); do
    if [[ -S "$MON" ]]; then
        break
    fi
    sleep 0.1
done
if [[ ! -S "$MON" ]]; then
    echo "capture_waypoints.sh: QEMU monitor socket ${MON} never appeared" >&2
    exit 1
fi

# Drive screendump via python3 (talks the QEMU monitor JSON-less
# command protocol over the unix socket).
screendump() {
    local ppm="$1"
    python3 - "$MON" "$ppm" <<'PY' >/dev/null
import socket, sys, time
mon_path, ppm_path = sys.argv[1], sys.argv[2]
s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
s.connect(mon_path)
time.sleep(0.3)
s.recv(65536)
s.sendall(f"screendump {ppm_path} vgpu 0\n".encode())
time.sleep(0.8)
try:
    s.recv(65536)
except OSError:
    pass
s.close()
PY
}

# Latest sv.time the serial log reports. The "think-census tic N
# sv.time=X.YYY" line fires on a sparse cadence (every 30 tics after
# the first 12), but the value still strictly increases.
latest_sv_time() {
    if [[ ! -s "$SERIAL" ]]; then
        echo "0"
        return
    fi
    grep -oE 'sv\.time=[0-9]+\.[0-9]+' "$SERIAL" 2>/dev/null \
        | tail -1 \
        | sed 's/sv\.time=//' \
        || echo "0"
}

# Poll until sv.time crosses each waypoint's mid-window threshold,
# then screendump. Threshold[i] = (i + 0.5) * PERIOD_SECONDS so the
# capture lands halfway through the window (no transition aliasing).
START_WALL=$(date +%s)
for i in $(seq 0 $((NUM_WAYPOINTS - 1))); do
    THRESHOLD=$(python3 -c "print(($i + 0.5) * $PERIOD_SECONDS)")
    printf -v idx '%02d' "$i"
    PPM="${PPM_PREFIX}-${idx}.ppm"
    echo "capture_waypoints.sh: waiting for sv.time >= ${THRESHOLD} (waypoint ${i})..."
    while :; do
        SV=$(latest_sv_time)
        if [[ -z "$SV" ]]; then
            SV="0"
        fi
        WALL=$(( $(date +%s) - START_WALL ))
        REACHED=$(python3 -c "print(1 if float('$SV') >= $THRESHOLD else 0)")
        if [[ "$REACHED" == "1" ]]; then
            echo "capture_waypoints.sh: sv.time=${SV} (wall=${WALL}s) -> capturing ${PPM}"
            screendump "$PPM"
            break
        fi
        if (( WALL >= MAX_WALL_SECS )); then
            echo "capture_waypoints.sh: timeout after ${WALL}s (sv.time=${SV}, needed ${THRESHOLD})" >&2
            screendump "$PPM"
            break
        fi
        sleep 1
    done
done

# Report.
echo
echo "capture_waypoints.sh: captured PPMs:"
for i in $(seq 0 $((NUM_WAYPOINTS - 1))); do
    printf -v idx '%02d' "$i"
    PPM="${PPM_PREFIX}-${idx}.ppm"
    if [[ -s "$PPM" ]]; then
        echo "  ${PPM} ($(wc -c < "$PPM") bytes)"
    else
        echo "  ${PPM} MISSING"
    fi
done
