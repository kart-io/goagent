#!/bin/bash
#
# create_release.sh - Helper script to create and push a release tag
#
# Usage:
#   ./create_release.sh <version> [type]
#
# Examples:
#   ./create_release.sh 1.2.3           # Create v1.2.3 release
#   ./create_release.sh 1.3.0 alpha     # Create v1.3.0-alpha.1
#   ./create_release.sh 1.3.0 beta      # Create v1.3.0-beta.1
#   ./create_release.sh 1.3.0 rc        # Create v1.3.0-rc.1
#

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

print_info() {
    echo -e "${BLUE}ℹ ${NC}$1"
}

print_success() {
    echo -e "${GREEN}✓ ${NC}$1"
}

print_warning() {
    echo -e "${YELLOW}⚠ ${NC}$1"
}

print_error() {
    echo -e "${RED}✗ ${NC}$1"
}

# Check arguments
if [ -z "$1" ]; then
    print_error "Version number is required"
    echo "Usage: $0 <version> [type]"
    echo "Example: $0 1.2.3 or $0 1.3.0 alpha"
    exit 1
fi

VERSION=$1
TYPE=${2:-"release"}

# Construct tag name
if [ "$TYPE" = "release" ]; then
    TAG="v${VERSION}"
else
    # Get next pre-release number
    EXISTING_TAGS=$(git tag -l "v${VERSION}-${TYPE}.*" | sort -V | tail -1)
    if [ -z "$EXISTING_TAGS" ]; then
        PRERELEASE_NUM=1
    else
        PRERELEASE_NUM=$(echo "$EXISTING_TAGS" | grep -oP "${TYPE}.\K\d+" | head -1)
        PRERELEASE_NUM=$((PRERELEASE_NUM + 1))
    fi
    TAG="v${VERSION}-${TYPE}.${PRERELEASE_NUM}"
fi

print_info "Preparing to create tag: ${TAG}"
echo

# Pre-flight checks
print_info "Running pre-flight checks..."

# Check if on main branch (for releases)
CURRENT_BRANCH=$(git rev-parse --abbrev-ref HEAD)
if [ "$TYPE" = "release" ] && [ "$CURRENT_BRANCH" != "main" ]; then
    print_warning "Not on main branch (current: ${CURRENT_BRANCH})"
    read -p "Continue anyway? (y/N) " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        print_error "Aborted"
        exit 1
    fi
fi

# Check for uncommitted changes
if ! git diff-index --quiet HEAD --; then
    print_error "You have uncommitted changes"
    git status --short
    exit 1
fi
print_success "No uncommitted changes"

# Pull latest changes
print_info "Pulling latest changes..."
git pull origin "$CURRENT_BRANCH"
print_success "Up to date with origin"

# Run tests
print_info "Running tests..."
if ! make test > /dev/null 2>&1; then
    print_error "Tests failed"
    read -p "Continue anyway? (y/N) " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        exit 1
    fi
else
    print_success "All tests passed"
fi

# Verify import layering
print_info "Verifying import layering..."
if ! ./verify_imports.sh > /dev/null 2>&1; then
    print_error "Import layering verification failed"
    exit 1
fi
print_success "Import layering verified"

# Check if CHANGELOG.md exists and has entry for this version
if [ -f "CHANGELOG.md" ]; then
    if grep -q "\[${VERSION}\]" CHANGELOG.md; then
        print_success "CHANGELOG.md has entry for ${VERSION}"
    else
        print_warning "CHANGELOG.md doesn't have entry for ${VERSION}"
        read -p "Continue anyway? (y/N) " -n 1 -r
        echo
        if [[ ! $REPLY =~ ^[Yy]$ ]]; then
            print_error "Please update CHANGELOG.md first"
            exit 1
        fi
    fi
fi

# Check if tag already exists
if git rev-parse "$TAG" >/dev/null 2>&1; then
    print_error "Tag ${TAG} already exists"
    exit 1
fi

echo
print_info "Tag to create: ${TAG}"
print_info "Current branch: ${CURRENT_BRANCH}"
print_info "Commit: $(git rev-parse --short HEAD)"
echo

# Ask for release notes
print_info "Enter release notes (press Ctrl+D when done):"
echo "---"
RELEASE_NOTES=$(cat)
echo "---"

# Confirm
echo
read -p "Create and push tag ${TAG}? (y/N) " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    print_error "Aborted"
    exit 1
fi

# Create tag
print_info "Creating tag ${TAG}..."
git tag -a "$TAG" -m "Release ${TAG}

${RELEASE_NOTES}"
print_success "Tag created locally"

# Push tag
print_info "Pushing tag to origin..."
git push origin "$TAG"
print_success "Tag pushed to origin"

echo
print_success "Release tag ${TAG} created successfully!"
echo
print_info "Next steps:"
echo "  1. Monitor the release workflow: https://github.com/kart-io/goagent/actions"
echo "  2. Check the release page: https://github.com/kart-io/goagent/releases/tag/${TAG}"
echo "  3. Verify pkg.go.dev updates: https://pkg.go.dev/github.com/kart-io/goagent@${TAG}"
echo

# If this is a release (not pre-release), remind about version bump
if [ "$TYPE" = "release" ]; then
    print_warning "Don't forget to:"
    echo "  - Announce the release"
    echo "  - Update documentation"
    echo "  - Close the milestone (if applicable)"
fi
