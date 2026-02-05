#!/bin/bash
# Runs SonarQube scanner using Docker
#
# Usage: ./scripts/run-sonar.sh
#
# Requirements:
# - Docker
# - sonar-project.properties in project root

set -e

SONAR_URL="${SONAR_URL:-http://sonarqube.s1.acolita.local}"
SONAR_TOKEN="${SONAR_TOKEN:?Set SONAR_TOKEN environment variable}"
PROJECT_DIR="$(cd "$(dirname "$0")/.." && pwd)"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${GREEN}Running SonarQube Scanner...${NC}"
echo "Project: ${PROJECT_DIR}"
echo "SonarQube: ${SONAR_URL}"
echo ""

# Run tests with coverage first
echo -e "${YELLOW}Running tests with coverage...${NC}"
cd "$PROJECT_DIR"
go test -coverprofile=coverage.out -json ./... > test-report.json 2>&1 || true

# Run SonarQube scanner via Docker
echo -e "${YELLOW}Running SonarQube scanner...${NC}"
docker run --rm \
    --network host \
    -e SONAR_HOST_URL="${SONAR_URL}" \
    -e SONAR_TOKEN="${SONAR_TOKEN}" \
    -v "${PROJECT_DIR}:/usr/src" \
    sonarsource/sonar-scanner-cli \
    -Dsonar.projectBaseDir=/usr/src

echo ""
echo -e "${GREEN}Scan complete!${NC}"
echo "View results: ${SONAR_URL}/dashboard?id=claude-shell-mcp"
echo ""
echo "To generate sonar-todo.md, run:"
echo "  ./scripts/sonar-issues.sh"
