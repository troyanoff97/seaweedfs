#!/bin/sh
# Loop-mount a persistent ext4 image at /vol for recovery-disk tests.
set -e

META=/meta
IMG="${META}/disk.img"
VOLMNT=/vol
SIZE_MB="${RECOVERY_DISK_MB:-128}"

setup_loop_data() {
  apk add --no-cache e2fsprogs util-linux >/dev/null 2>&1 || true
  mkdir -p "$META" "$VOLMNT"

  if [ ! -f "$IMG" ]; then
    echo "Creating ${SIZE_MB}M ext4 image at ${IMG}..."
    dd if=/dev/zero of="$IMG" bs=1M count="$SIZE_MB" status=none
    mkfs.ext4 -F -m 0 "$IMG" >/dev/null
  fi

  if ! mountpoint -q "$VOLMNT" 2>/dev/null; then
    mount -o loop "$IMG" "$VOLMNT"
  fi
}

if [ "$1" = "volume" ]; then
  setup_loop_data
fi

exec /entrypoint.sh "$@"
