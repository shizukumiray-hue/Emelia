#!/bin/bash
# demo_test.sh - Quick demo of the Telegram fetch feature

set -e

echo "=========================================="
echo "  EMILIA TELEGRAM FETCHER - DEMO TEST"
echo "=========================================="
echo ""

# Test 1: Fetch from Telegram
echo "🧪 Test 1: Fetching from Telegram channel..."
./emilia -fetch zip_cm_edu_kg -output Data/demo_fetch.txt
echo ""

# Test 2: Check output
echo "🧪 Test 2: Verifying fetched data..."
if [ -f "Data/demo_fetch.txt" ]; then
    COUNT=$(wc -l < Data/demo_fetch.txt)
    echo "✅ Fetched $COUNT proxies"
    echo "📄 Sample (first 5 lines):"
    head -5 Data/demo_fetch.txt
else
    echo "❌ Fetch failed"
    exit 1
fi
echo ""

# Test 3: Merge test
echo "🧪 Test 3: Testing merge functionality..."
python3 gabung.py Data/demo_fetch.txt Data/proxy-rabu-16-agustus.txt -o Data/demo_merged.txt
echo ""

# Test 4: Scanner test (limited)
echo "🧪 Test 4: Testing scanner with 5 proxies..."
head -5 Data/demo_merged.txt > Data/demo_sample.txt
./emilia -input Data/demo_sample.txt -limit 5
echo ""

echo "=========================================="
echo "✅ ALL TESTS PASSED!"
echo "=========================================="
echo ""
echo "📌 You can now use:"
echo "   • ./fetch_and_scan.sh (full workflow)"
echo "   • ./emilia -fetch CHANNEL (fetch only)"
echo "   • ./emilia -input FILE (scan only)"
echo ""

# Cleanup
rm -f Data/demo_*.txt
echo "🧹 Cleaned up test files"
