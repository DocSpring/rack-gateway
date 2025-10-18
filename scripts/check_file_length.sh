#!/bin/bash
# Check file length limits: 500 for code, 1000 for test files
# Applies to ALL tracked files with exclusions listed below

set -e

# Exclusion patterns (paths or glob patterns to skip)
EXCLUSIONS=(
  "vendor/*"
  "goober/*"
  "node_modules/*"
  "web/dist/*"
  "web/node_modules/*"
  "bin/*"
  ".jscpd/*"
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
)

exitcode=0

# Function to check if file should be excluded
should_exclude() {
  local file="$1"
  for pattern in "${EXCLUSIONS[@]}"; do
    # Remove leading ./ from file path for matching
    local clean_file="${file#./}"
    if [[ "$clean_file" == $pattern ]]; then
      return 0
    fi
  done
  return 1
}

# Get all tracked files from git
while IFS= read -r file; do
  # Skip if file doesn't exist or is excluded
  if [[ ! -f "$file" ]] || should_exclude "$file"; then
    continue
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

    # Some file types get 1000 line limit even for non-tests
    if [[ "$file" == *Taskfile* ]] || \
       [[ "$file" == *CLAUDE.md ]] || \
       [[ "$file" == *README.md ]]; then
      max_lines=1000
    fi

    if [ "$lines" -gt "$max_lines" ]; then
      echo "❌ $file has $lines lines (max $max_lines allowed)"
      exitcode=1
    fi
  fi
done < <(git ls-files)

if [ $exitcode -eq 0 ]; then
  echo "✅ All files within length limits"
fi

exit $exitcode
