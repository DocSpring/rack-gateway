#!/usr/bin/env bash
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to show usage
usage() {
    echo "Usage: $0 {major|minor|patch}"
    echo ""
    echo "Bump types:"
    echo "  major   - Bump major version (1.0.0 -> 2.0.0)"
    echo "  minor   - Bump minor version (1.0.0 -> 1.1.0)"
    echo "  patch   - Bump patch version (1.0.0 -> 1.0.1)"
    exit 1
}

# Function to bump semantic version
bump_version() {
    local version=$1
    local bump_type=$2

    # Parse version
    IFS='.' read -r major minor patch <<< "$version"

    case "$bump_type" in
        major)
            major=$((major + 1))
            minor=0
            patch=0
            ;;
        minor)
            minor=$((minor + 1))
            patch=0
            ;;
        patch)
            patch=$((patch + 1))
            ;;
    esac

    echo "${major}.${minor}.${patch}"
}

# Function to get current version from package.json
get_version() {
    jq -r '.version' web/package.json
}

# Function to update version in package.json
update_version() {
    local new_version=$1
    echo -e "${BLUE}Updating version to v${new_version}...${NC}"

    cd web
    # Update package.json using jq
    jq ".version = \"${new_version}\"" package.json > package.json.tmp
    mv package.json.tmp package.json

    # Update lockfile
    bun install
    cd ..

    echo -e "${GREEN}✓ Version updated to v${new_version}${NC}"
}

# Check arguments
if [ $# -ne 1 ]; then
    usage
fi

BUMP_TYPE=$1

# Validate bump type
if [[ ! "$BUMP_TYPE" =~ ^(major|minor|patch)$ ]]; then
    echo -e "${RED}Error: Invalid bump type: $BUMP_TYPE${NC}"
    usage
fi

# Get current version and calculate new version
current_version=$(get_version)
new_version=$(bump_version "$current_version" "$BUMP_TYPE")

echo -e "${YELLOW}Version: ${current_version} -> ${new_version}${NC}"
update_version "$new_version"

echo ""
echo -e "${GREEN}Version bump complete!${NC}"
echo ""
echo "Next steps:"
echo "1. Review the changes: git diff"
echo "2. Commit: git commit -am \"chore: bump version to v${new_version}\""
echo "3. Create release tag: ./scripts/create-release-tags.sh"
echo "4. Push: git push && git push --tags"
