#!/usr/bin/env bash

set -euo pipefail

if [[ $# -gt 0 ]]; then
  mapfile -t files < <(printf '%s\n' "$@")
else
  mapfile -t files < <(git ls-files)
fi

for file in "${files[@]}"; do
  if [[ ! -f "$file" ]]; then
    continue
  fi

  basename="$(basename "$file")"
  if [[ "$basename" == .* ]]; then
    continue
  fi

  if [[ "$OSTYPE" == darwin* ]]; then
    sed -i '' 's/[[:space:]]*$//' "$file"
  else
    sed -i 's/[[:space:]]*$//' "$file"
  fi

  if [[ -s "$file" ]] && [[ $(tail -c1 "$file" | wc -l) -eq 0 ]]; then
    echo >> "$file"
  fi
done
