#!/bin/bash
# fetch_and_scan_ci.sh - CI-optimized workflow for GitHub Actions
# This script runs headless without interactive prompts

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

CHANNEL="${TELEGRAM_CHANNEL:-zip_cm_edu_kg}"
TELEGRAM_FILE="Data/telegram_fetch.txt"
EXISTING_FILE="Data/proxy-rabu-16-agustus.txt"
MERGED_FILE="Data/merged_input.txt"
OUTPUT_FILE="Data/alive.txt"

# Start timestamp
START_TIME=$(date +%s)

echo "=========================================="
echo "  EMILIA PROXY FETCHER & SCANNER (CI)"
echo "=========================================="
echo "Environment: CI/CD"
echo "Channel: @${CHANNEL}"
echo "Timestamp: $(date -u +'%Y-%m-%d %H:%M:%S UTC')"
echo "=========================================="
echo ""

# Step 1: Fetch from Telegram
echo "📡 [1/4] Fetching proxies from Telegram..."
FETCH_SUCCESS=false
if ./emilia -fetch "$CHANNEL" -output "$TELEGRAM_FILE" 2>&1; then
    if [ -f "$TELEGRAM_FILE" ] && [ -s "$TELEGRAM_FILE" ]; then
        TELEGRAM_COUNT=$(wc -l < "$TELEGRAM_FILE" | tr -d ' ')
        echo "✅ Fetched $TELEGRAM_COUNT proxies from Telegram"
        FETCH_SUCCESS=true
    else
        echo "⚠️  Telegram fetch produced empty file"
    fi
else
    echo "⚠️  Telegram fetch failed (continuing with existing proxies)"
fi
echo ""

# Step 2: Merge with existing proxies
echo "🔗 [2/4] Merging proxies..."
if [ "$FETCH_SUCCESS" = true ]; then
    python3 gabung.py "$TELEGRAM_FILE" "$EXISTING_FILE" -o "$MERGED_FILE"
    INPUT_FILE="$MERGED_FILE"
    MERGED_COUNT=$(wc -l < "$MERGED_FILE" | tr -d ' ')
    echo "✅ Merged: $MERGED_COUNT unique proxies"
else
    echo "ℹ️  Using existing file only: $EXISTING_FILE"
    INPUT_FILE="$EXISTING_FILE"
    MERGED_COUNT=$(wc -l < "$EXISTING_FILE" | tr -d ' ')
fi
echo ""

# Step 3: Scan and validate proxies
echo "🔍 [3/4] Scanning and validating proxies..."
echo "⏳ Input: $MERGED_COUNT proxies"
echo ""

SCAN_START=$(date +%s)
./emilia -input "$INPUT_FILE"
SCAN_END=$(date +%s)
SCAN_DURATION=$((SCAN_END - SCAN_START))

echo ""

# Step 4: Report
echo "✅ [4/4] Complete!"
echo ""
echo "=========================================="
echo "📊 FINAL REPORT"
echo "=========================================="

if [ -f "$OUTPUT_FILE" ]; then
    ALIVE_COUNT=$(wc -l < "$OUTPUT_FILE" | tr -d ' ')
    SUCCESS_RATE=$(awk "BEGIN {printf \"%.2f\", ($ALIVE_COUNT / $MERGED_COUNT) * 100}")
    
    echo "✓ Telegram fetch: ${TELEGRAM_COUNT:-0} proxies"
    echo "✓ Total merged: $MERGED_COUNT proxies"
    echo "✓ Alive proxies: $ALIVE_COUNT"
    echo "✓ Success rate: ${SUCCESS_RATE}%"
    echo "✓ Scan duration: $((SCAN_DURATION / 60))m $((SCAN_DURATION % 60))s"
else
    echo "⚠️  No alive proxies found"
    ALIVE_COUNT=0
fi

END_TIME=$(date +%s)
TOTAL_DURATION=$((END_TIME - START_TIME))

echo "✓ Total runtime: $((TOTAL_DURATION / 60))m $((TOTAL_DURATION % 60))s"
echo "✓ Output file: $OUTPUT_FILE"
echo "=========================================="
echo ""

# Exit with appropriate code
if [ "$ALIVE_COUNT" -gt 0 ]; then
    echo "🎉 Success!"
    exit 0
else
    echo "⚠️  Warning: No alive proxies found"
    exit 1
fi
