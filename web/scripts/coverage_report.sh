#!/bin/bash
# Show uncovered lines from go coverage report

set -e

COVERAGE_FILE="${1:-coverage.out}"
TARGET_FILE="${2:-}"

if [ ! -f "$COVERAGE_FILE" ]; then
  echo "Error: Coverage file not found: $COVERAGE_FILE"
  echo "Usage: $0 [coverage_file] [target_file]"
  exit 1
fi

echo "=== Coverage Report ==="
echo ""

# Show total coverage
echo "Total Coverage:"
go tool cover -func="$COVERAGE_FILE" | grep "total:" | sed 's/total:/  Total:/'
echo ""

# Show uncovered functions (anything below 100%)
echo "Functions below 100% coverage:"
go tool cover -func="$COVERAGE_FILE" | \
  grep -v "total:" | \
  grep -v "100.0%" | \
  while IFS= read -r line; do
    echo "  $line"
  done

echo ""

# If target file specified, show actual uncovered lines
if [ -n "$TARGET_FILE" ]; then
  echo "=== Uncovered lines in $TARGET_FILE ==="
  echo ""

  # Parse coverage file to find uncovered line ranges for the target file
  # Format: file:start.col,end.col statements count
  grep "$TARGET_FILE" "$COVERAGE_FILE" | while read -r line; do
    # Extract components
    range=$(echo "$line" | cut -d: -f2 | cut -d' ' -f1)
    count=$(echo "$line" | awk '{print $NF}')

    # Only show lines with 0 coverage
    if [ "$count" = "0" ]; then
      # Parse line range (e.g., "10.5,12.3" means lines 10-12)
      start_line=$(echo "$range" | cut -d',' -f1 | cut -d'.' -f1)
      end_line=$(echo "$range" | cut -d',' -f2 | cut -d'.' -f1)

      # Extract just the filename from the full path for display
      # But use TARGET_FILE for reading since we're in that directory
      for line_num in $(seq "$start_line" "$end_line"); do
        line_content=$(sed -n "${line_num}p" "$TARGET_FILE")
        printf "  Line %4d: %s\n" "$line_num" "$line_content"
      done
    fi
  done
else
  echo "=== Run with filename argument to see uncovered lines ==="
  echo "Example: $0 $COVERAGE_FILE internal/gateway/auth/session.go"
fi
