#!/bin/bash
# fetch_and_scan.sh - Complete workflow for fetching from Telegram and scanning proxies

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

CHANNEL="zip_cm_edu_kg"
TELEGRAM_FILE="Data/telegram_fetch.txt"
EXISTING_FILE="Data/proxy-rabu-16-agustus.txt"
MERGED_FILE="Data/merged_input.txt"
OUTPUT_FILE="Data/alive.txt"

echo "=========================================="
echo "  EMILIA PROXY FETCHER & SCANNER"
echo "=========================================="
echo ""

# Step 1: Fetch from Telegram
echo "📡 Step 1/4: Fetching proxies from Telegram channel @${CHANNEL}..."
if go run . -fetch "$CHANNEL" -output "$TELEGRAM_FILE"; then
    if [ -f "$TELEGRAM_FILE" ]; then
        TELEGRAM_COUNT=$(wc -l < "$TELEGRAM_FILE" | tr -d ' ')
        echo "✅ Fetched $TELEGRAM_COUNT proxies from Telegram"
    else
        echo "⚠️  No proxies fetched from Telegram, using existing file only"
        TELEGRAM_FILE=""
    fi
else
    echo "⚠️  Telegram fetch failed, continuing with existing proxies only"
    TELEGRAM_FILE=""
fi
echo ""

# Step 2: Merge with existing proxies
echo "🔗 Step 2/4: Merging proxies..."
if [ -n "$TELEGRAM_FILE" ] && [ -f "$TELEGRAM_FILE" ]; then
    python3 gabung.py "$TELEGRAM_FILE" "$EXISTING_FILE" -o "$MERGED_FILE"
    INPUT_FILE="$MERGED_FILE"
else
    echo "ℹ️  Using existing file only: $EXISTING_FILE"
    INPUT_FILE="$EXISTING_FILE"
fi
echo ""

# Step 3: Scan and validate proxies
echo "🔍 Step 3/4: Scanning and validating proxies..."
echo "⏳ This may take a while depending on the number of proxies..."
echo ""
go run . -input "$INPUT_FILE"
echo ""

# Step 4: Report
echo "✅ Step 4/4: Complete!"
if [ -f "$OUTPUT_FILE" ]; then
    ALIVE_COUNT=$(wc -l < "$OUTPUT_FILE" | tr -d ' ')
    echo ""
    echo "=========================================="
    echo "📊 FINAL REPORT"
    echo "=========================================="
    echo "✓ Alive proxies: $ALIVE_COUNT"
    echo "✓ Output file: $OUTPUT_FILE"
    echo "=========================================="
else
    echo "⚠️  No alive proxies found"
fi
echo ""
echo "🎉 Done!"
