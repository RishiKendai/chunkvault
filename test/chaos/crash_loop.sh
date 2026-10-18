#!/usr/bin/env bash
# Kills the backup at random moments, then proves the repo recovers and a full backup restores identically.
set -u
SRC=${SRC:-/tmp/crash-src}
REPO=cv-crash
OUT=cv-crash-out
BIN=./chunkvault

if [ ! -d "$SRC" ]; then
  mkdir -p "$SRC"
  for i in 1 2 3; do dd if=/dev/urandom of="$SRC/f$i.bin" bs=1m count=100 2>/dev/null; done
fi
rm -rf "$REPO"

for i in $(seq 1 20); do
  $BIN backup --repo "$REPO" "$SRC" >/dev/null 2>&1 &
  pid=$!
  sleep 0.$((RANDOM % 9 + 1))
  kill -9 $pid 2>/dev/null
  wait $pid 2>/dev/null
  # reopening runs recovery; verify must not error on whatever snapshots exist
  $BIN verify --repo "$REPO" --read-data >/dev/null || { echo "FAIL: verify after kill #$i"; exit 1; }
done

$BIN backup --repo "$REPO" "$SRC" || { echo "FAIL: final backup"; exit 1; }
rm -rf "$OUT"
$BIN restore --repo "$REPO" latest "$OUT" || { echo "FAIL: restore"; exit 1; }
diff -r "$SRC" "$OUT" && $BIN verify --repo "$REPO" --read-data && echo "PASS: 20 kills, repo recovered, restore identical"