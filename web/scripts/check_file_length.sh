#!/bin/bash
# Check file length limits: 500 for code, 1000 for test files

set -e

exitcode=0

# Check Go files
while IFS= read -r file; do
  lines=$(wc -l < "$file")
  if [[ "$file" == *_test.go ]]; then
    # Test files can be up to 1000 lines
    if [ "$lines" -gt 1000 ]; then
      echo "❌ $file has $lines lines (max 1000 allowed for test files)"
      exitcode=1
    fi
  else
    # Regular code files max 500 lines
    if [ "$lines" -gt 500 ]; then
      echo "❌ $file has $lines lines (max 500 allowed)"
      exitcode=1
    fi
  fi
done < <(find . -name "*.go" -type f -not -path "./vendor/*" -not -path "./goober/*")

# Check TypeScript/TSX files
while IFS= read -r file; do
  lines=$(wc -l < "$file")
  if [[ "$file" == *.test.ts* ]]; then
    # Test files can be up to 1000 lines
    if [ "$lines" -gt 1000 ]; then
      echo "❌ $file has $lines lines (max 1000 allowed for test files)"
      exitcode=1
    fi
  else
    # Regular code files max 500 lines
    if [ "$lines" -gt 500 ]; then
      echo "❌ $file has $lines lines (max 500 allowed)"
      exitcode=1
    fi
  fi
done < <(find web/src -name "*.ts" -o -name "*.tsx" -type f 2>/dev/null || true)

if [ $exitcode -eq 0 ]; then
  echo "✅ All files within length limits"
fi

exit $exitcode
