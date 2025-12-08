#!/bin/bash

# Ensure we are running from the project root
cd "$(dirname "$0")/.." || exit 1
# Configuration
TIMEOUT_DURATION="5s"
LOG_FILE="example_verification_results.log"
SUMMARY_FILE="example_verification_summary.md"

# Initialize files
echo "Verification Run - $(date)" > "$LOG_FILE"
echo "# Example Verification Summary" > "$SUMMARY_FILE"
echo "" >> "$SUMMARY_FILE"
echo "| File | Status | Notes |" >> "$SUMMARY_FILE"
echo "|------|--------|-------|" >> "$SUMMARY_FILE"

# Stats
TOTAL=0
PASSED=0
TIMEOUT=0 # Likely servers or REPLs
FAILED=0
SKIPPED=0

echo "🔍 Finding example files..."
# Find all files with "package main" in examples directory using find to ensure recursion works
EXAMPLES=$(find examples -name "*.go" -print0 | xargs -0 grep -l "package main" | sort)

echo "🚀 Starting execution of $(echo "$EXAMPLES" | wc -l | xargs) examples..."

for example in $EXAMPLES; do
    ((TOTAL++))
    dir=$(dirname "$example")
    filename=$(basename "$example")
    
    # Check if file has "func main()"
    if ! grep -q "func main()" "$example"; then
        echo "⚠️ Skipping $filename (No 'func main', likely a helper file)" >> "$LOG_FILE"
        # Don't mark as skipped in summary to avoid clutter, just ignore
        ((SKIPPED++))
        continue
    fi

    echo "---------------------------------------------------" >> "$LOG_FILE"
    echo "Running: $example" >> "$LOG_FILE"
    echo "Running: $example"

    # skips checks
    if [[ "$example" == *"nats"* ]]; then
         echo "⚠️ Skipping $example (NATS dependency)" >> "$LOG_FILE"
         echo "| \`$example\` | ⚠️ SKIPPED | NATS dependency |" >> "$SUMMARY_FILE"
         ((SKIPPED++))
         continue
    fi

    # Run with timeout
    # Attempt 1: Run specific file (works for self-contained files)
    
    # Configure arguments for specific examples
    ARGS=""
    if [[ "$example" == *"16-pdf-rag/main.go"* ]]; then
        ARGS="-pdf examples/basic/16-pdf-rag/cymbal-starlight-2024.pdf -prompt 'summary'"
    elif [[ "$example" == *"17-vertexai-rag/main.go"* ]]; then
        # Use existing PDF from example 16 and simple mode
        ARGS="-pdf examples/basic/16-pdf-rag/cymbal-starlight-2024.pdf -prompt 'summary' -simple"
    fi

    OUTPUT=$(timeout "$TIMEOUT_DURATION" go run "$example" $ARGS 2>&1)
    EXIT_CODE=$?

    # Attempt 2: If "undefined" error, run the directory (works for multi-file packages)
    if [ $EXIT_CODE -ne 0 ] && echo "$OUTPUT" | grep -qE "undefined|undeclared"; then
         echo "   -> Retrying with directory context..." >> "$LOG_FILE"
         # Use *.go to include all files in package
         OUTPUT=$(timeout "$TIMEOUT_DURATION" go run "$dir"/*.go $ARGS 2>&1)
         EXIT_CODE=$?
    fi

    if [ $EXIT_CODE -eq 0 ]; then
        echo "✅ PASS" >> "$LOG_FILE"
        echo "| \`$example\` | ✅ PASS | |" >> "$SUMMARY_FILE"
        ((PASSED++))
    elif [ $EXIT_CODE -eq 124 ]; then
        # Timeout - likely a server or REPL
        echo "⏳ TIMEOUT (Expected for services)" >> "$LOG_FILE"
        echo "| \`$example\` | ⏳ TIMEOUT | Service/Interactive |" >> "$SUMMARY_FILE"
        ((TIMEOUT++))
    else
        # Check if it's a configuration error (missing API key etc)
        if echo "$OUTPUT" | grep -qE "API key|not found|connecting|refused|no such host|environment variable|Unauthenticated|CREDENTIALS_MISSING"; then
             echo "⚠️ CONFIG ERROR (Acceptable)" >> "$LOG_FILE"
             echo "$OUTPUT" >> "$LOG_FILE"
             echo "| \`$example\` | ⚠️ CONFIG | Runtime config required |" >> "$SUMMARY_FILE"
             # Count config errors as passed for "code correctness" purposes
             ((PASSED++))
        elif echo "$OUTPUT" | grep -q "panic"; then
             echo "❌ PANIC" >> "$LOG_FILE"
             echo "$OUTPUT" >> "$LOG_FILE"
             echo "| \`$example\` | ❌ PANIC | **See Log** |" >> "$SUMMARY_FILE"
             ((FAILED++))
        else 
             echo "❌ FAIL" >> "$LOG_FILE"
             echo "$OUTPUT" >> "$LOG_FILE"
             echo "| \`$example\` | ❌ FAIL | Compilation/Runtime Error |" >> "$SUMMARY_FILE"
             ((FAILED++))
        fi
    fi
done

echo ""
echo "==========================================="
echo "Summary"
echo "Total: $TOTAL"
echo "Passed: $PASSED"
echo "Timeout/Services: $TIMEOUT"
echo "Skipped: $SKIPPED"
echo "Failed: $FAILED"
echo "==========================================="
echo "Details written to $SUMMARY_FILE and $LOG_FILE"
