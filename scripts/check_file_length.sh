#!/bin/bash
# Check file length limits: 500 for code, 1000 for test files
# Applies to ALL tracked text files with exclusions listed below
#
# Usage:
#   scripts/check_file_length.sh [files...]
#   If no files provided, checks all tracked files

set -e

# Exclusion patterns (paths or glob patterns to skip)
EXCLUSIONS=(
  "*.md"
  "*.json"
  "*.yml"
  "*.yaml"
  "*.sql"
  "*.txt"
  "*.lock"
  "go.sum"
  "pnpm-lock.yaml"
  "package-lock.json"
  "*.generated.*"
  "*/generated/*"
  "web/src/api/openapi.json"
  "web/src/api/generated.ts"
  "web/src/api/types.generated.ts"
  "web/src/lib/generated/*"
  "internal/gateway/openapi/generated/*"
  # Binary file extensions
  "*.jpg"
  "*.jpeg"
  "*.png"
  "*.gif"
  "*.ico"
  "*.svg"
  "*.woff"
  "*.woff2"
  "*.ttf"
  "*.eot"
  "*.pdf"
  "*.zip"
  "*.tar"
  "*.gz"
  "*.tgz"
  "*.bz2"
  "*.xz"
  "*.exe"
  "*.dll"
  "*.so"
  "*.dylib"
  "*.bin"
  "*.dat"
)

exitcode=0

# Function to check if file should be excluded
should_exclude() {
  local file="$1"
  for pattern in "${EXCLUSIONS[@]}"; do
    # Remove leading ./ from file path for matching
    local clean_file="${file#./}"
    # shellcheck disable=SC2053
    if [[ "$clean_file" == $pattern ]]; then  # We want glob matching here
      return 0
    fi
  done
  return 1
}

# Check a single file
check_file() {
  local file="$1"

  # Skip if file doesn't exist or is excluded
  if [[ ! -f "$file" ]] || should_exclude "$file"; then
    return 0
  fi

  lines=$(wc -l < "$file" 2>/dev/null || echo "0")

  # Determine if this is a test file
  is_test=false
  if [[ "$file" == *_test.go ]] || \
     [[ "$file" == *.test.ts ]] || \
     [[ "$file" == *.test.tsx ]] || \
     [[ "$file" == *.spec.ts ]] || \
     [[ "$file" == *.spec.tsx ]] || \
     [[ "$file" == **/e2e/** ]]; then
    is_test=true
  fi

  if [ "$is_test" = true ]; then
    # Test files can be up to 1000 lines
    if [ "$lines" -gt 1000 ]; then
      echo "❌ $file has $lines lines (max 1000 allowed for test files)"
      exitcode=1
    fi
  else
    # Regular files max 500 lines (or 1000 for config/data files)
    max_lines=500
    min_lines=10

    # Some file types get 1000 line limit even for non-tests
    if [[ "$file" == *Taskfile* ]] || \
       [[ "$file" == *CLAUDE.md ]] || \
       [[ "$file" == *README.md ]]; then
      max_lines=1000
    fi

    # Check minimum for TSX/JSX files (no lazy re-export files)
    if [[ "$file" == *.tsx ]] || [[ "$file" == *.jsx ]]; then
      if [ "$lines" -lt "$min_lines" ]; then
        echo "❌ $file has only $lines lines (min $min_lines required for .tsx/.jsx files - no lazy re-exports)"
        exitcode=1
      fi
    fi

    if [ "$lines" -gt "$max_lines" ]; then
      echo "❌ $file has $lines lines (max $max_lines allowed)"
      exitcode=1
    fi
  fi
}

# If files provided as arguments, check only those
if [ $# -gt 0 ]; then
  for file in "$@"; do
    check_file "$file"
  done
else
  # Otherwise check all tracked files
  while IFS= read -r file; do
    check_file "$file"
  done < <(git ls-files)
fi

if [ $exitcode -eq 0 ]; then
  echo "✅ All files within length limits"
fi

exit $exitcode
