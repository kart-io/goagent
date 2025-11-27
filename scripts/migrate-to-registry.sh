#!/bin/bash
# GoAgent Provider Import Migration Script
# 自动将项目从旧的 provider 导入方式迁移到新的方式
#
# 使用方法:
#   ./migrate-to-registry.sh [目录路径]
#
# 示例:
#   ./migrate-to-registry.sh ./examples
#   ./migrate-to-registry.sh .

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 显示使用帮助
show_help() {
    cat << EOF
GoAgent Provider Registry 迁移工具

用法: $0 [选项] [目录]

选项:
  -h, --help          显示此帮助信息
  -d, --dry-run       仅显示将要进行的更改，不实际修改文件
  -b, --backup        在修改前创建备份文件 (.bak)
  -v, --verbose       显示详细输出
  --no-registry       仅更新导入路径，不迁移到 registry 模式

目录:
  要迁移的目录路径 (默认: 当前目录)

示例:
  $0 ./examples                    # 迁移 examples 目录
  $0 -b ./myproject                # 迁移并创建备份
  $0 -d .                          # 预览将要进行的更改
  $0 --no-registry ./examples     # 仅更新导入路径

详细说明:
  默认迁移模式会将代码从旧的 providers 包迁移到 Registry 方式。
  使用 --no-registry 选项可以仅更新导入路径到新的 contrib 包。

EOF
}

# 默认参数
DRY_RUN=false
BACKUP=false
VERBOSE=false
USE_REGISTRY=true
TARGET_DIR="."

# 解析命令行参数
while [[ $# -gt 0 ]]; do
    case $1 in
        -h|--help)
            show_help
            exit 0
            ;;
        -d|--dry-run)
            DRY_RUN=true
            shift
            ;;
        -b|--backup)
            BACKUP=true
            shift
            ;;
        -v|--verbose)
            VERBOSE=true
            shift
            ;;
        --no-registry)
            USE_REGISTRY=false
            shift
            ;;
        -*)
            echo -e "${RED}错误: 未知选项 $1${NC}"
            show_help
            exit 1
            ;;
        *)
            TARGET_DIR=$1
            shift
            ;;
    esac
done

# 验证目录
if [ ! -d "$TARGET_DIR" ]; then
    echo -e "${RED}错误: 目录不存在: $TARGET_DIR${NC}"
    exit 1
fi

echo -e "${BLUE}=== GoAgent Provider Registry 迁移工具 ===${NC}"
echo -e "${BLUE}目标目录: ${TARGET_DIR}${NC}"
echo -e "${BLUE}模式: $([ "$USE_REGISTRY" = true ] && echo "Registry 模式" || echo "仅更新导入")${NC}"
echo -e "${BLUE}Dry run: $([ "$DRY_RUN" = true ] && echo "是" || echo "否")${NC}"
echo ""

# 统计变量
TOTAL_FILES=0
MODIFIED_FILES=0
BACKUP_FILES=0

# Provider 映射表
declare -A PROVIDER_MAP=(
    ["openai"]="OpenAI"
    ["deepseek"]="DeepSeek"
    ["gemini"]="Gemini"
    ["anthropic"]="Anthropic"
    ["cohere"]="Cohere"
    ["huggingface"]="HuggingFace"
    ["ollama"]="Ollama"
    ["kimi"]="Kimi"
    ["siliconflow"]="SiliconFlow"
)

# 备份文件
backup_file() {
    local file=$1
    if [ "$BACKUP" = true ] && [ "$DRY_RUN" = false ]; then
        cp "$file" "$file.bak"
        ((BACKUP_FILES++))
        [ "$VERBOSE" = true ] && echo -e "${GREEN}  备份: $file.bak${NC}"
    fi
}

# 检查文件是否需要迁移
needs_migration() {
    local file=$1
    grep -q '"github.com/kart-io/goagent/llm/providers"' "$file" 2>/dev/null
}

# 迁移到 Registry 模式
migrate_to_registry() {
    local file=$1
    local temp_file="${file}.tmp"
    local modified=false

    [ "$VERBOSE" = true ] && echo -e "${YELLOW}处理文件: $file${NC}"

    # 创建临时文件
    cp "$file" "$temp_file"

    # 检查是否导入了 providers 包
    if grep -q '"github.com/kart-io/goagent/llm/providers"' "$temp_file"; then
        modified=true

        # 添加必要的导入
        if ! grep -q '"github.com/kart-io/goagent/llm/registry"' "$temp_file"; then
            # 在 import 块中添加 registry 和 constants
            sed -i '/import (/a\\t"github.com/kart-io/goagent/llm/constants"\n\t"github.com/kart-io/goagent/llm/registry"' "$temp_file"
        fi

        # 为每个 provider 添加空白导入
        for provider in "${!PROVIDER_MAP[@]}"; do
            local provider_name="${PROVIDER_MAP[$provider]}"

            # 检查是否使用了这个 provider
            if grep -q "providers\.New${provider_name}WithOptions" "$temp_file"; then
                # 添加空白导入
                if ! grep -q "_ \"github.com/kart-io/goagent/contrib/llm-providers/${provider}\"" "$temp_file"; then
                    sed -i "/import (/a\\t_ \"github.com/kart-io/goagent/contrib/llm-providers/${provider}\"" "$temp_file"
                fi

                # 替换函数调用为 registry.New
                sed -i "s/providers\.New${provider_name}WithOptions(\([^)]*\))/registry.New(constants.Provider${provider_name}, \1)/g" "$temp_file"

                [ "$VERBOSE" = true ] && echo -e "${GREEN}  迁移 ${provider_name} 到 registry${NC}"
            fi
        done

        # 可以删除旧的 providers 导入（如果不再需要）
        if ! grep -q 'providers\.' "$temp_file"; then
            sed -i '/"github.com\/kart-io\/goagent\/llm\/providers"/d' "$temp_file"
        fi
    fi

    if [ "$modified" = true ]; then
        if [ "$DRY_RUN" = false ]; then
            backup_file "$file"
            mv "$temp_file" "$file"
            echo -e "${GREEN}✓ 已迁移: $file${NC}"
        else
            echo -e "${YELLOW}[DRY RUN] 将迁移: $file${NC}"
            rm "$temp_file"
        fi
        ((MODIFIED_FILES++))
    else
        rm "$temp_file"
        [ "$VERBOSE" = true ] && echo -e "  跳过（无需迁移）: $file"
    fi
}

# 仅更新导入路径
update_imports_only() {
    local file=$1
    local temp_file="${file}.tmp"
    local modified=false

    [ "$VERBOSE" = true ] && echo -e "${YELLOW}更新导入: $file${NC}"

    cp "$file" "$temp_file"

    # 检查并替换每个 provider 的导入
    for provider in "${!PROVIDER_MAP[@]}"; do
        local provider_name="${PROVIDER_MAP[$provider]}"

        # 如果使用了这个 provider，添加新的导入
        if grep -q "providers\.New${provider_name}WithOptions" "$temp_file"; then
            modified=true

            # 添加 contrib provider 导入
            if ! grep -q "\"github.com/kart-io/goagent/contrib/llm-providers/${provider}\"" "$temp_file"; then
                sed -i "/import (/a\\t\"github.com/kart-io/goagent/contrib/llm-providers/${provider}\"" "$temp_file"
            fi

            # 替换函数调用
            sed -i "s/providers\.New${provider_name}WithOptions(\([^)]*\))/${provider}.New(\1)/g" "$temp_file"

            [ "$VERBOSE" = true ] && echo -e "${GREEN}  更新 ${provider_name} 导入${NC}"
        fi
    done

    # 删除旧的 providers 导入
    if [ "$modified" = true ]; then
        sed -i '/"github.com\/kart-io\/goagent\/llm\/providers"/d' "$temp_file"

        if [ "$DRY_RUN" = false ]; then
            backup_file "$file"
            mv "$temp_file" "$file"
            echo -e "${GREEN}✓ 已更新: $file${NC}"
        else
            echo -e "${YELLOW}[DRY RUN] 将更新: $file${NC}"
            rm "$temp_file"
        fi
        ((MODIFIED_FILES++))
    else
        rm "$temp_file"
        [ "$VERBOSE" = true ] && echo -e "  跳过（无需更新）: $file"
    fi
}

# 处理单个文件
process_file() {
    local file=$1
    ((TOTAL_FILES++))

    if needs_migration "$file"; then
        if [ "$USE_REGISTRY" = true ]; then
            migrate_to_registry "$file"
        else
            update_imports_only "$file"
        fi
    fi
}

# 查找并处理所有 Go 文件
echo -e "${BLUE}扫描 Go 文件...${NC}"
while IFS= read -r -d '' file; do
    # 跳过 vendor 和 .git 目录
    if [[ $file == *"/vendor/"* ]] || [[ $file == *"/.git/"* ]]; then
        continue
    fi

    process_file "$file"
done < <(find "$TARGET_DIR" -name "*.go" -type f -print0)

echo ""
echo -e "${BLUE}=== 迁移完成 ===${NC}"
echo -e "${BLUE}扫描文件数: ${TOTAL_FILES}${NC}"
echo -e "${GREEN}修改文件数: ${MODIFIED_FILES}${NC}"
[ "$BACKUP" = true ] && echo -e "${YELLOW}备份文件数: ${BACKUP_FILES}${NC}"

if [ "$DRY_RUN" = true ]; then
    echo ""
    echo -e "${YELLOW}这是 dry run 模式，没有实际修改文件${NC}"
    echo -e "${YELLOW}移除 -d 或 --dry-run 选项以执行实际迁移${NC}"
fi

if [ "$BACKUP" = true ] && [ "$MODIFIED_FILES" -gt 0 ]; then
    echo ""
    echo -e "${YELLOW}提示: 备份文件已创建 (.bak)${NC}"
    echo -e "${YELLOW}如需恢复，运行: find ${TARGET_DIR} -name '*.bak' -exec bash -c 'mv \"\$0\" \"\${0%.bak}\"' {} \;${NC}"
fi

if [ "$MODIFIED_FILES" -gt 0 ] && [ "$DRY_RUN" = false ]; then
    echo ""
    echo -e "${GREEN}迁移完成！请运行以下命令验证:${NC}"
    echo -e "${GREEN}  cd ${TARGET_DIR}${NC}"
    echo -e "${GREEN}  go mod tidy${NC}"
    echo -e "${GREEN}  go test ./...${NC}"
fi

exit 0
