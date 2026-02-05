#!/bin/bash
# Fetches SonarQube issues and generates sonar-todo.md
#
# Usage: ./scripts/sonar-issues.sh [-x] [sonarqube-url]
#
# Options:
#   -x    Enable debug mode (bash -x)
#
# Requirements:
# - curl
# - jq
# - SonarQube scanner must have been run first (see: run-sonar.sh)

# Parse flags
while getopts "x" opt; do
    case $opt in
        x) set -x ;;
        *) ;;
    esac
done
shift $((OPTIND-1))

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

SONAR_URL="${1:-http://sonarqube.s1.acolita.local}"
SONAR_TOKEN="squ_0aa9f2b907a0c3c33bf243e7d5af4749437072e6"
PROJECT_KEY="claude-shell-mcp"
OUTPUT_FILE="sonar-todo.md"

AUTH_HEADER="Authorization: Bearer ${SONAR_TOKEN}"

echo -e "${GREEN}Fetching issues from SonarQube...${NC}"
echo "URL: ${SONAR_URL}/api/issues/search?componentKeys=${PROJECT_KEY}"

# Fetch all issues (paginated)
fetch_issues() {
    local page=1
    local page_size=500
    local all_issues="[]"

    while true; do
        response=$(curl -s -H "${AUTH_HEADER}" "${SONAR_URL}/api/issues/search?componentKeys=${PROJECT_KEY}&ps=${page_size}&p=${page}&statuses=OPEN,CONFIRMED,REOPENED")

        # Check for errors
        if echo "$response" | jq -e '.errors' > /dev/null 2>&1; then
            echo -e "${RED}Error fetching issues:${NC}"
            echo "$response" | jq '.errors'
            exit 1
        fi

        issues=$(echo "$response" | jq '.issues')
        total=$(echo "$response" | jq '.total')

        if [ "$(echo "$issues" | jq 'length')" -eq 0 ]; then
            break
        fi

        all_issues=$(echo "$all_issues $issues" | jq -s 'add')

        # Check if we have all issues
        current_count=$(echo "$all_issues" | jq 'length')
        if [ "$current_count" -ge "$total" ]; then
            break
        fi

        page=$((page + 1))
    done

    echo "$all_issues"
}

# Fetch security hotspots
fetch_hotspots() {
    curl -s -H "${AUTH_HEADER}" "${SONAR_URL}/api/hotspots/search?projectKey=${PROJECT_KEY}&status=TO_REVIEW" | jq '.hotspots // []'
}

# Generate markdown
generate_markdown() {
    local issues="$1"
    local hotspots="$2"
    local date=$(date +%Y-%m-%d)

    cat << EOF
# SonarQube Issues Checklist

*Generated from scan on ${date}*
*Source: ${SONAR_URL}/dashboard?id=${PROJECT_KEY}*

EOF

    # Security Hotspots
    hotspot_count=$(echo "$hotspots" | jq 'length')
    echo "## Security Hotspots (${hotspot_count})"
    echo ""
    if [ "$hotspot_count" -gt 0 ]; then
        echo "$hotspots" | jq -r '.[] | "- [ ] `\(.component | split(":")[1]):\(.line // "?")` - \(.message) (\(.vulnerabilityProbability))"'
    else
        echo "_No security hotspots found._"
    fi
    echo ""

    # Cognitive Complexity Issues
    complexity_issues=$(echo "$issues" | jq '[.[] | select(.rule | contains("cognitive-complexity"))]')
    complexity_count=$(echo "$complexity_issues" | jq 'length')

    echo "## Cognitive Complexity Issues (${complexity_count})"
    echo ""
    if [ "$complexity_count" -gt 0 ]; then
        echo "$complexity_issues" | jq -r 'sort_by(.message | capture("(?<num>[0-9]+)") | .num | tonumber) | reverse | .[] | "- [ ] `\(.component | split(":")[1]):\(.line)` - \(.message)"'
    else
        echo "_No cognitive complexity issues found._"
    fi
    echo ""

    # Duplicated Literals
    literal_issues=$(echo "$issues" | jq '[.[] | select(.rule | contains("duplicated-string-literal") or contains("duplicate-string"))]')
    literal_count=$(echo "$literal_issues" | jq 'length')

    echo "## Duplicated Literals (${literal_count})"
    echo ""
    if [ "$literal_count" -gt 0 ]; then
        echo "$literal_issues" | jq -r 'group_by(.component) | .[] | "### \(.[0].component | split(":")[1])", (.[] | "- [ ] `\(.line)` - \(.message)"), ""'
    else
        echo "_No duplicated literal issues found._"
    fi
    echo ""

    # Other Code Smells
    other_issues=$(echo "$issues" | jq '[.[] | select((.rule | contains("cognitive-complexity") | not) and (.rule | contains("duplicated-string") | not) and (.rule | contains("duplicate-string") | not))]')
    other_count=$(echo "$other_issues" | jq 'length')

    echo "## Other Issues (${other_count})"
    echo ""
    if [ "$other_count" -gt 0 ]; then
        echo "$other_issues" | jq -r 'group_by(.rule) | .[] | "### \(.[0].rule)", (.[] | "- [ ] `\(.component | split(":")[1]):\(.line // "?")` - \(.message)"), ""'
    else
        echo "_No other issues found._"
    fi
    echo ""

    # Summary
    total=$((hotspot_count + complexity_count + literal_count + other_count))
    echo "---"
    echo "**Total: ${total} issues**"
}

# Main
echo -e "${YELLOW}Fetching issues...${NC}"
issues=$(fetch_issues)
issue_count=$(echo "$issues" | jq 'length')
echo -e "Found ${GREEN}${issue_count}${NC} issues"

echo -e "${YELLOW}Fetching security hotspots...${NC}"
hotspots=$(fetch_hotspots)
hotspot_count=$(echo "$hotspots" | jq 'length')
echo -e "Found ${GREEN}${hotspot_count}${NC} security hotspots"

echo -e "${YELLOW}Generating ${OUTPUT_FILE}...${NC}"
generate_markdown "$issues" "$hotspots" > "$OUTPUT_FILE"

echo -e "${GREEN}Done!${NC} Output written to ${OUTPUT_FILE}"
echo ""
echo "Summary:"
echo "  - Security Hotspots: ${hotspot_count}"
echo "  - Total Issues: ${issue_count}"
echo ""
echo "View full report: ${SONAR_URL}/dashboard?id=${PROJECT_KEY}"
