#!/bin/bash
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Get the version from web/package.json
VERSION=$(jq -r '.version' web/package.json)

if [ -z "$VERSION" ] || [ "$VERSION" = "null" ]; then
  echo -e "${RED}Error: Could not read version from web/package.json${NC}"
  exit 1
fi

echo -e "${GREEN}Current version: ${VERSION}${NC}"
echo ""

# Create tag name
TAG="v${VERSION}"

# Check if tag already exists
if git rev-parse "$TAG" >/dev/null 2>&1; then
  echo -e "${YELLOW}Tag ${TAG} already exists${NC}"
  exit 0
fi

echo -e "${GREEN}Creating tag: ${TAG}${NC}"
git tag -a "$TAG" -m "Release ${TAG}"

echo ""
echo -e "${GREEN}Tag created successfully!${NC}"
echo ""
echo -e "${YELLOW}To push the tag to remote, run:${NC}"
echo -e "  git push origin ${TAG}"
echo ""
echo -e "${YELLOW}Or push all tags:${NC}"
echo -e "  git push --tags"
