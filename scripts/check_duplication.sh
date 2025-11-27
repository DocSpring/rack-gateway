#!/bin/bash
set -e

TYPE=$1

if [ "$TYPE" == "go" ]; then
  TARGET_DIR="internal"
  LABEL="Go"
elif [ "$TYPE" == "web" ]; then
  TARGET_DIR="web"
  LABEL="Web"
else
  echo "Usage: $0 [go|web]"
  exit 1
fi

# Ensure we are in the project root
# Assuming this script is run from project root as scripts/check_duplication.sh

if [ ! -d "$TARGET_DIR" ]; then
  echo "Error: Directory $TARGET_DIR does not exist."
  exit 1
fi

cd "$TARGET_DIR"

set -o pipefail
# Run jscpd on the current directory (target dir) so it picks up local config (.jscpd.json)
if output=$(jscpd . 2>&1); then
  echo "No duplicate $LABEL code detected."
  exit 0
else
  exit_code=$?
  echo "$LABEL code duplication detected:"
  echo "$output"
  exit $exit_code
fi
