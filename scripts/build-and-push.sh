#!/bin/bash
set -e

# Docker Hub image name
IMAGE_NAME="docker.io/docspringcom/rack-gateway"

# Get commit SHA from git
COMMIT_SHA=$(git rev-parse --short HEAD)

echo "Building rack-gateway for Docker Hub..."
echo "Image: ${IMAGE_NAME}"
echo "Commit: ${COMMIT_SHA}"

# Build for amd64 (linux/amd64)
# Use DOCKER_DEFAULT_PLATFORM to ensure amd64 build
DOCKER_DEFAULT_PLATFORM=linux/amd64 docker build \
  --tag "${IMAGE_NAME}:${COMMIT_SHA}" \
  --tag "${IMAGE_NAME}:latest" \
  .

echo ""
echo "Build complete! Images tagged:"
echo "  - ${IMAGE_NAME}:${COMMIT_SHA}"
echo "  - ${IMAGE_NAME}:latest"

echo ""
echo "Pushing to Docker Hub..."
docker push "${IMAGE_NAME}:${COMMIT_SHA}"
docker push "${IMAGE_NAME}:latest"

echo ""
echo "Push complete!"
echo ""
echo "To deploy, update convox.yml to use: image: ${IMAGE_NAME}:${COMMIT_SHA}"
echo "Then run: convox deploy"
