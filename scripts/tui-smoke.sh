#!/usr/bin/env bash
set -euo pipefail

smoke_dir=$(mktemp -d)
export ALIVYN_HOME="$smoke_dir/state"
export ALIVYN_API_KEY=test-key

go build -o "$smoke_dir/alivyn" ./cmd/alivyn
printf '\n\n\n' | "$smoke_dir/alivyn" init > "$smoke_dir/init-output"

if [[ $(uname -s) == Darwin ]]; then
  (sleep 1; printf '\033') | script -q "$smoke_dir/terminal-output" "$smoke_dir/alivyn" tui > /dev/null
else
  (sleep 1; printf '\033') | script -q -c "$smoke_dir/alivyn tui" "$smoke_dir/terminal-output" > /dev/null
fi

grep -q 'An AI individual' "$smoke_dir/terminal-output"
echo 'TUI terminal smoke check passed'
