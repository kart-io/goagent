#!/bin/bash
# Coverage Gate Script
# 检查测试覆盖率是否满足最低要求

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 整体覆盖率阈值
OVERALL_THRESHOLD=45

# 关键文件列表（文件名:最低覆盖率阈值）
# 格式: "文件路径:阈值"
declare -A CRITICAL_FILES=(
    ["llm/providers/openai.go"]=70
    ["llm/providers/gemini.go"]=65
    ["llm/common/base.go"]=50
    ["tools/practical/database_query.go"]=40
)

# 检查覆盖率文件是否存在
if [ ! -f "coverage.out" ]; then
    echo -e "${RED}❌ 错误: coverage.out 文件不存在${NC}"
    echo "请先运行: go test -coverprofile=coverage.out ./..."
    exit 1
fi

echo "======================================"
echo "       测试覆盖率检查"
echo "======================================"
echo ""

# 1. 检查整体覆盖率
echo "📊 检查整体覆盖率..."
OVERALL_COVERAGE=$(go tool cover -func=coverage.out | grep "^total:" | awk '{print $3}' | sed 's/%//')

if [ -z "$OVERALL_COVERAGE" ]; then
    echo -e "${RED}❌ 无法获取整体覆盖率${NC}"
    exit 1
fi

echo "   整体覆盖率: ${OVERALL_COVERAGE}%"
echo "   最低要求: ${OVERALL_THRESHOLD}%"

if (( $(echo "$OVERALL_COVERAGE < $OVERALL_THRESHOLD" | bc -l) )); then
    echo -e "${RED}❌ 整体覆盖率未达标！${NC}"
    OVERALL_PASS=0
else
    echo -e "${GREEN}✅ 整体覆盖率达标${NC}"
    OVERALL_PASS=1
fi

echo ""

# 2. 检查关键文件覆盖率
echo "🎯 检查关键文件覆盖率..."
CRITICAL_PASS=1

for file in "${!CRITICAL_FILES[@]}"; do
    threshold=${CRITICAL_FILES[$file]}

    # 检查文件是否存在
    if [ ! -f "$file" ]; then
        echo -e "${YELLOW}⚠️  文件不存在: $file (跳过)${NC}"
        continue
    fi

    # 获取文件覆盖率（计算该文件所有函数的平均覆盖率）
    FILE_COVERAGE=$(go tool cover -func=coverage.out | grep "github.com/kart-io/goagent/$file:" | awk '{sum+=$3; count++} END {if (count>0) print sum/count; else print 0}' | sed 's/%//')

    if [ -z "$FILE_COVERAGE" ] || [ "$FILE_COVERAGE" = "0" ]; then
        echo -e "${RED}❌ $file: 无覆盖率数据 (要求 ${threshold}%)${NC}"
        CRITICAL_PASS=0
        continue
    fi

    FILE_COVERAGE=$(printf "%.1f" "$FILE_COVERAGE")

    if (( $(echo "$FILE_COVERAGE < $threshold" | bc -l) )); then
        echo -e "${RED}❌ $file: ${FILE_COVERAGE}% (要求 ${threshold}%)${NC}"
        CRITICAL_PASS=0
    else
        echo -e "${GREEN}✅ $file: ${FILE_COVERAGE}% (要求 ${threshold}%)${NC}"
    fi
done

echo ""

# 3. 显示模块覆盖率摘要
echo "📈 模块覆盖率摘要:"
echo ""

# llm/providers 模块
PROVIDERS_COVERAGE=$(go tool cover -func=coverage.out | grep "^github.com/kart-io/goagent/llm/providers/" | grep -v "_test.go" | awk '{sum+=$3; count++} END {if (count>0) print sum/count; else print 0}')
if [ -n "$PROVIDERS_COVERAGE" ] && [ "$PROVIDERS_COVERAGE" != "0" ]; then
    PROVIDERS_COVERAGE=$(printf "%.1f" "$PROVIDERS_COVERAGE")
    echo "   llm/providers/: ${PROVIDERS_COVERAGE}%"
fi

# tools 模块
TOOLS_COVERAGE=$(go tool cover -func=coverage.out | grep "^github.com/kart-io/goagent/tools/" | grep -v "_test.go" | awk '{sum+=$3; count++} END {if (count>0) print sum/count; else print 0}')
if [ -n "$TOOLS_COVERAGE" ] && [ "$TOOLS_COVERAGE" != "0" ]; then
    TOOLS_COVERAGE=$(printf "%.1f" "$TOOLS_COVERAGE")
    echo "   tools/: ${TOOLS_COVERAGE}%"
fi

# agents 模块
AGENTS_COVERAGE=$(go tool cover -func=coverage.out | grep "^github.com/kart-io/goagent/agents/" | grep -v "_test.go" | awk '{sum+=$3; count++} END {if (count>0) print sum/count; else print 0}')
if [ -n "$AGENTS_COVERAGE" ] && [ "$AGENTS_COVERAGE" != "0" ]; then
    AGENTS_COVERAGE=$(printf "%.1f" "$AGENTS_COVERAGE")
    echo "   agents/: ${AGENTS_COVERAGE}%"
fi

echo ""
echo "======================================"

# 4. 最终判定
if [ $OVERALL_PASS -eq 1 ] && [ $CRITICAL_PASS -eq 1 ]; then
    echo -e "${GREEN}✅ 覆盖率检查通过！${NC}"
    exit 0
else
    echo -e "${RED}❌ 覆盖率检查失败！${NC}"
    echo ""
    echo "请确保："
    echo "  1. 整体覆盖率 ≥ ${OVERALL_THRESHOLD}%"
    echo "  2. 所有关键文件达到各自的覆盖率要求："
    for file in "${!CRITICAL_FILES[@]}"; do
        threshold=${CRITICAL_FILES[$file]}
        echo "     - $file ≥ ${threshold}%"
    done
    exit 1
fi
