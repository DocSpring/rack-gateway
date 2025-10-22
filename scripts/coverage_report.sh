#!/usr/bin/env bash
# Coverage report helper with configurable minimum threshold
set -euo pipefail

COVERAGE_FILE="${1:-coverage.out}"
TARGET_FILE="${2:-}"
THRESHOLD_RAW="${COVERAGE_THRESHOLD:-80}"

# Validate coverage threshold input
if [[ ! $THRESHOLD_RAW =~ ^[0-9]+([.][0-9]+)?$ ]]; then
  echo "Error: COVERAGE_THRESHOLD must be numeric (got '$THRESHOLD_RAW')." >&2
  exit 1
fi

THRESHOLD="$THRESHOLD_RAW"

if [[ ! -f "$COVERAGE_FILE" ]]; then
  echo "Error: Coverage file not found: $COVERAGE_FILE" >&2
  echo "Usage: $0 [coverage_file] [target_file]" >&2
  exit 1
fi

COVERAGE_DATA=$(go tool cover -func="$COVERAGE_FILE")
TOTAL_LINE=$(printf '%s\n' "$COVERAGE_DATA" | grep 'total:')
TOTAL_PERCENT=$(printf '%s\n' "$TOTAL_LINE" | awk '{print $3}' | tr -d '%')

# Determine pass/fail before printing detailed output
if awk -v t="$THRESHOLD" -v c="$TOTAL_PERCENT" 'BEGIN { exit (c+0 >= t+0 ? 0 : 1) }'; then
  COVERAGE_OK=1
else
  COVERAGE_OK=0
fi

printf '=== Coverage Report ===\n\n'
printf 'Minimum required: %.2f%%\n' "$THRESHOLD"
printf 'Total coverage : %.2f%%\n\n' "$TOTAL_PERCENT"

LOW_FUNCTIONS=$(printf '%s\n' "$COVERAGE_DATA" | awk -v t="$THRESHOLD" '
  /total:/ { next }
  {
    percent = $3
    sub("%", "", percent)
    if ((percent + 0) < (t + 0)) {
      printf("  %s\n", $0)
    }
  }
')

if [[ -n "$LOW_FUNCTIONS" ]]; then
  printf 'Functions below %.2f%% coverage:\n' "$THRESHOLD"
  printf '%s\n' "$LOW_FUNCTIONS"
else
  printf 'All functions meet the %.2f%% coverage requirement.\n' "$THRESHOLD"
fi

if [[ -n "$TARGET_FILE" ]]; then
  printf '\n=== Uncovered lines in %s ===\n\n' "$TARGET_FILE"
  MATCHES=$(grep "$TARGET_FILE" "$COVERAGE_FILE" || true)
  if [[ -z "$MATCHES" ]]; then
    printf 'No uncovered ranges recorded for %s.\n' "$TARGET_FILE"
  else
    while IFS= read -r line; do
      range=$(printf '%s' "$line" | cut -d: -f2 | cut -d' ' -f1)
      count=$(printf '%s' "$line" | awk '{print $NF}')
      if [[ "$count" = "0" ]]; then
        start_line=$(printf '%s' "$range" | cut -d',' -f1 | cut -d'.' -f1)
        end_line=$(printf '%s' "$range" | cut -d',' -f2 | cut -d'.' -f1)
        for line_num in $(seq "$start_line" "$end_line"); do
          line_content=$(sed -n "${line_num}p" "$TARGET_FILE")
          printf '  Line %4d: %s\n' "$line_num" "$line_content"
        done
      fi
    done <<< "$MATCHES"
  fi
else
  printf '\nProvide a target filename to view uncovered lines.\n'
  printf 'Example: %s %s internal/gateway/auth/session.go\n' "$0" "$COVERAGE_FILE"
fi

if [[ $COVERAGE_OK -eq 1 ]]; then
  exit 0
fi

printf '\n❌ Coverage %.2f%% is below required %.2f%%.\n' "$TOTAL_PERCENT" "$THRESHOLD"
exit 1
