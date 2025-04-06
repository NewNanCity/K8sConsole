#!/bin/bash
set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m' # 无色

# 检测操作系统类型
detect_os() {
    case "$(uname -s)" in
    Darwin*) echo "darwin" ;;
    Linux*) echo "linux" ;;
    MINGW* | MSYS* | CYGWIN*) echo "windows" ;;
    *) echo "unknown" ;;
    esac
}

OS_TYPE=$(detect_os)

# 根据操作系统设置默认缓存路径
if [ "$OS_TYPE" = "windows" ]; then
    DEFAULT_CACHE_DIR="${TEMP:-/tmp}/docker-buildx-cache"
elif [ "$OS_TYPE" = "darwin" ]; then
    DEFAULT_CACHE_DIR="/tmp/docker-buildx-cache"
else
    DEFAULT_CACHE_DIR="/var/cache/docker-buildx"
fi

# 打印帮助信息
print_help() {
    echo -e "${BLUE}跨平台 Go 编译脚本${NC}"
    echo "使用 crazymax/goxx 镜像进行 Go 代码的跨平台编译"
    echo ""
    echo "用法:"
    echo "  $0 -f <入口文件.go> -p <平台> [-o <输出目录>] [-v <Go版本>] [-c <是否启用CGO>] [--cache-dir <缓存目录>]"
    echo ""
    echo "选项:"
    echo "  -f <入口文件>      指定要编译的 Go 入口文件 (必需)"
    echo "  -p <平台>         指定目标平台，例如 linux/amd64,darwin/arm64 (必需)"
    echo "  -o <输出目录>      指定输出目录 (默认: ./dist)"
    echo "  -v <Go版本>       指定 Go 版本 (默认: 1.24.1)"
    echo "  -c <是否启用CGO>   是否启用 CGO，值为 0 或 1 (默认: 1)"
    echo "  --cache-dir <目录> 指定 Docker buildx 缓存目录 (默认: 根据操作系统自动设置)"
    echo "  --no-cache        禁用 Docker buildx 缓存"
    echo "  -h               显示帮助信息"
    echo ""
    echo "支持的平台例子:"
    echo "  darwin/amd64, darwin/arm64, linux/386, linux/amd64, linux/arm64,"
    echo "  linux/arm/v5, linux/arm/v6, linux/arm/v7, windows/386, windows/amd64,"
    echo "  linux/mips, linux/mipsle, linux/mips64, linux/mips64le,"
    echo "  linux/ppc64le, linux/riscv64, linux/s390x"
    echo ""
    echo "示例:"
    echo "  $0 -f mccli.go -p linux/amd64,windows/amd64,darwin/amd64 -o ./bin"
    echo "  $0 -f mccli.go -p linux/arm64 -v 1.20 -c 0 -o ./output"
    echo "  $0 -f mccli.go -p windows/amd64 --cache-dir D:/temp/buildcache"
}

# 默认参数
GO_VERSION="1.24.1"
OUTPUT_DIR="./dist"
CGO_ENABLED="1"
ENTRY_FILE=""
PLATFORMS=""
CACHE_DIR="$DEFAULT_CACHE_DIR"
USE_CACHE=true

# 解析命令行参数
while [[ $# -gt 0 ]]; do
    case $1 in
    -f)
        ENTRY_FILE="$2"
        shift 2
        ;;
    -p)
        PLATFORMS="$2"
        shift 2
        ;;
    -o)
        OUTPUT_DIR="$2"
        shift 2
        ;;
    -v)
        GO_VERSION="$2"
        shift 2
        ;;
    -c)
        CGO_ENABLED="$2"
        shift 2
        ;;
    --cache-dir)
        CACHE_DIR="$2"
        shift 2
        ;;
    --no-cache)
        USE_CACHE=false
        shift
        ;;
    -h)
        print_help
        exit 0
        ;;
    *)
        echo -e "${RED}无效的选项: $1${NC}" 1>&2
        print_help
        exit 1
        ;;
    esac
done

# 检查必需参数
if [ -z "$ENTRY_FILE" ]; then
    echo -e "${RED}错误: 必须指定入口文件 (-f 选项)${NC}" 1>&2
    print_help
    exit 1
fi

if [ -z "$PLATFORMS" ]; then
    echo -e "${RED}错误: 必须指定目标平台 (-p 选项)${NC}" 1>&2
    print_help
    exit 1
fi

# 检查入口文件是否存在
if [ ! -f "$ENTRY_FILE" ]; then
    echo -e "${RED}错误: 入口文件 '$ENTRY_FILE' 不存在${NC}" 1>&2
    exit 1
fi

# 准备输出目录
mkdir -p "$OUTPUT_DIR"

# 如果启用缓存，准备缓存目录
if [ "$USE_CACHE" = true ]; then
    mkdir -p "$CACHE_DIR"
    echo -e "${BLUE}[INFO]${NC} 使用缓存目录: $CACHE_DIR"
fi

# 获取项目名称（从入口文件名或目录名）
PROJECT_NAME=$(basename "$ENTRY_FILE" .go)
if [ "$PROJECT_NAME" = "$ENTRY_FILE" ]; then
    PROJECT_NAME=$(basename "$(pwd)")
fi

# 获取入口文件相对路径
RELATIVE_ENTRY_FILE=$(realpath --relative-to="$(pwd)" "$ENTRY_FILE")
ENTRY_DIR=$(dirname "$RELATIVE_ENTRY_FILE")

echo -e "${BLUE}[INFO]${NC} 项目名称: $PROJECT_NAME"
echo -e "${BLUE}[INFO]${NC} 入口文件: $RELATIVE_ENTRY_FILE"
echo -e "${BLUE}[INFO]${NC} 目标平台: $PLATFORMS"
echo -e "${BLUE}[INFO]${NC} 输出目录: $OUTPUT_DIR"
echo -e "${BLUE}[INFO]${NC} Go 版本: $GO_VERSION"
echo -e "${BLUE}[INFO]${NC} CGO 启用状态: $CGO_ENABLED"
echo -e "${BLUE}[INFO]${NC} 操作系统类型: $OS_TYPE"

# 创建临时Dockerfile
TEMP_DOCKERFILE=$(mktemp -t dockerfile.XXXXXX)
cat >"$TEMP_DOCKERFILE" <<EOD
# syntax=docker/dockerfile:1

FROM --platform=\$BUILDPLATFORM crazymax/goxx:$GO_VERSION AS base
ENV CGO_ENABLED=$CGO_ENABLED
WORKDIR /go/src/$PROJECT_NAME

FROM base AS build
ARG TARGETPLATFORM
# 解析TARGETPLATFORM变量，提取操作系统和架构
RUN echo "TARGETPLATFORM: \$TARGETPLATFORM" && \\
    export OS=\$(echo \$TARGETPLATFORM | cut -d/ -f1) && \\
    export ARCH=\$(echo \$TARGETPLATFORM | cut -d/ -f2) && \\
    echo "OS: \$OS, ARCH: \$ARCH" && \\
    echo "export TARGET_OS=\$OS" > /tmp/env && \\
    echo "export TARGET_ARCH=\$ARCH" >> /tmp/env

RUN --mount=type=cache,sharing=private,target=/var/cache/apt \\
    --mount=type=cache,sharing=private,target=/var/lib/apt/lists \\
    goxx-apt-get install -y binutils gcc g++ pkg-config

# 构建并设置输出文件名为 PROJECT_NAME_OS_ARCH
RUN --mount=type=bind,source=.,target=/go/src/$PROJECT_NAME \\
    --mount=type=cache,target=/root/.cache \\
    --mount=type=cache,target=/go/pkg/mod \\
    . /tmp/env && \\
    OUTPUT_NAME="${PROJECT_NAME}_\${TARGET_OS}_\${TARGET_ARCH}" && \\
    echo "Building: \$OUTPUT_NAME" && \\
    goxx-go build -o /out/\$OUTPUT_NAME ./$RELATIVE_ENTRY_FILE && \\
    # Windows平台需要.exe扩展名
    if [ "\$TARGET_OS" = "windows" ]; then \\
      cp /out/\$OUTPUT_NAME /out/\$OUTPUT_NAME.exe; \\
    fi

FROM scratch AS artifact
COPY --from=build /out /
EOD

echo -e "${GREEN}[✓]${NC} 创建临时Dockerfile成功"

# 检查是否需要darwin平台支持
if [[ "$PLATFORMS" == *"darwin"* ]]; then
    echo -e "${YELLOW}[!]${NC} 检测到需要编译 darwin 平台，正在添加 osxcross 支持"
    sed -i '/FROM base AS build/a COPY --from=crazymax\/osxcross:11.3 \/osxcross \/osxcross' "$TEMP_DOCKERFILE"
fi

# 运行Docker buildx构建
echo -e "${BLUE}[INFO]${NC} 开始构建..."

# 确保有Docker buildx
if ! docker buildx version >/dev/null 2>&1; then
    echo -e "${RED}错误: Docker buildx 未找到。请确保您的 Docker 版本支持 buildx 功能。${NC}" 1>&2
    exit 1
fi

# 检查是否有可用的buildx构建器
BUILDER_NAME="gobuilder"

# 检查是否已经有默认构建器
if docker buildx ls | grep -q '(default)'; then
    echo -e "${BLUE}[INFO]${NC} 已找到默认的 buildx 构建器，将使用它"
# 检查是否已经存在名为gobuilder的构建器
elif docker buildx ls | grep -q "$BUILDER_NAME"; then
    echo -e "${BLUE}[INFO]${NC} 找到现有的 '$BUILDER_NAME' 构建器，将使用它"
    docker buildx use "$BUILDER_NAME"
else
    echo -e "${YELLOW}[!]${NC} 没有发现默认的 buildx 构建器，正在创建新的构建器..."
    docker buildx create --name "$BUILDER_NAME" --use
    echo -e "${GREEN}[✓]${NC} buildx 构建器 '$BUILDER_NAME' 创建成功"
fi

# 构建命令基础部分
BUILD_CMD="docker buildx build"

# 添加缓存配置
if [ "$USE_CACHE" = true ]; then
    BUILD_CMD="$BUILD_CMD --cache-from=type=local,src=$CACHE_DIR --cache-to=type=local,dest=$CACHE_DIR,mode=max"
else
    BUILD_CMD="$BUILD_CMD --no-cache"
fi

# 执行构建
$BUILD_CMD \
    --platform "$PLATFORMS" \
    --output "type=local,dest=$OUTPUT_DIR" \
    --target "artifact" \
    --file "$TEMP_DOCKERFILE" \
    .

BUILD_STATUS=$?

# 清理临时文件
rm "$TEMP_DOCKERFILE"

# 检查构建结果
if [ $BUILD_STATUS -eq 0 ]; then
    echo -e "\n${GREEN}[✓] 构建成功!${NC}"
    echo -e "${BLUE}[INFO]${NC} 编译结果位于: $OUTPUT_DIR"

    # 后处理：将子目录中的文件移动到顶层，保持输出目录结构扁平
    echo -e "${BLUE}[INFO]${NC} 正在整理输出目录结构..."

    # 遍历所有平台目录
    for platform in $(echo $PLATFORMS | tr ',' ' '); do
        platform_dir=$(echo $platform | tr '/' '_')
        OS=$(echo $platform | cut -d/ -f1)
        ARCH=$(echo $platform | cut -d/ -f2)
        expected_name="${PROJECT_NAME}_${OS}_${ARCH}"

        # 检查平台子目录是否存在
        if [ -d "$OUTPUT_DIR/$platform_dir" ]; then
            # 对于Windows平台的特殊处理
            if [ "$OS" = "windows" ]; then
                # 如果存在.exe文件，优先移动它
                if [ -f "$OUTPUT_DIR/$platform_dir/$expected_name.exe" ]; then
                    echo -e "  - ${YELLOW}[移动]${NC} $OUTPUT_DIR/$platform_dir/$expected_name.exe -> $OUTPUT_DIR/$expected_name.exe"
                    mv "$OUTPUT_DIR/$platform_dir/$expected_name.exe" "$OUTPUT_DIR/$expected_name.exe"
                    # 如果同时存在无扩展名文件，直接删除它
                    if [ -f "$OUTPUT_DIR/$platform_dir/$expected_name" ]; then
                        echo -e "  - ${YELLOW}[删除]${NC} $OUTPUT_DIR/$platform_dir/$expected_name (使用.exe版本)"
                        rm "$OUTPUT_DIR/$platform_dir/$expected_name"
                    fi
                # 如果只有无扩展名文件，将其重命名为.exe
                elif [ -f "$OUTPUT_DIR/$platform_dir/$expected_name" ]; then
                    echo -e "  - ${YELLOW}[移动+添加.exe]${NC} $OUTPUT_DIR/$platform_dir/$expected_name -> $OUTPUT_DIR/$expected_name.exe"
                    mv "$OUTPUT_DIR/$platform_dir/$expected_name" "$OUTPUT_DIR/$expected_name.exe"
                fi

                # 同样处理旧格式文件名
                if [ -f "$OUTPUT_DIR/$platform_dir/$PROJECT_NAME.exe" ]; then
                    echo -e "  - ${YELLOW}[移动+重命名]${NC} $OUTPUT_DIR/$platform_dir/$PROJECT_NAME.exe -> $OUTPUT_DIR/$expected_name.exe"
                    mv "$OUTPUT_DIR/$platform_dir/$PROJECT_NAME.exe" "$OUTPUT_DIR/$expected_name.exe"
                    # 如果同时存在无扩展名文件，直接删除它
                    if [ -f "$OUTPUT_DIR/$platform_dir/$PROJECT_NAME" ]; then
                        echo -e "  - ${YELLOW}[删除]${NC} $OUTPUT_DIR/$platform_dir/$PROJECT_NAME (使用.exe版本)"
                        rm "$OUTPUT_DIR/$platform_dir/$PROJECT_NAME"
                    fi
                elif [ -f "$OUTPUT_DIR/$platform_dir/$PROJECT_NAME" ]; then
                    echo -e "  - ${YELLOW}[移动+重命名+添加.exe]${NC} $OUTPUT_DIR/$platform_dir/$PROJECT_NAME -> $OUTPUT_DIR/$expected_name.exe"
                    mv "$OUTPUT_DIR/$platform_dir/$PROJECT_NAME" "$OUTPUT_DIR/$expected_name.exe"
                fi
            # 非Windows平台的处理
            else
                # 检查子目录中的新格式文件
                if [ -f "$OUTPUT_DIR/$platform_dir/$expected_name" ]; then
                    echo -e "  - ${YELLOW}[移动]${NC} $OUTPUT_DIR/$platform_dir/$expected_name -> $OUTPUT_DIR/$expected_name"
                    mv "$OUTPUT_DIR/$platform_dir/$expected_name" "$OUTPUT_DIR/$expected_name"
                fi

                # 检查子目录中的旧格式文件
                if [ -f "$OUTPUT_DIR/$platform_dir/$PROJECT_NAME" ]; then
                    echo -e "  - ${YELLOW}[移动+重命名]${NC} $OUTPUT_DIR/$platform_dir/$PROJECT_NAME -> $OUTPUT_DIR/$expected_name"
                    mv "$OUTPUT_DIR/$platform_dir/$PROJECT_NAME" "$OUTPUT_DIR/$expected_name"
                fi
            fi

            # 清理子目录
            echo -e "  - ${YELLOW}[清理]${NC} 删除子目录: $OUTPUT_DIR/$platform_dir"
            rm -rf "$OUTPUT_DIR/$platform_dir"
        fi
    done

    # 显示最终编译结果
    echo -e "${BLUE}[INFO]${NC} 编译结果:"
    for platform in $(echo $PLATFORMS | tr ',' ' '); do
        OS=$(echo $platform | cut -d/ -f1)
        ARCH=$(echo $platform | cut -d/ -f2)
        expected_name="${PROJECT_NAME}_${OS}_${ARCH}"

        # Windows平台检查.exe文件
        if [ "$OS" = "windows" ] && [ -f "$OUTPUT_DIR/$expected_name.exe" ]; then
            echo -e "  - ${GREEN}$platform${NC}: $OUTPUT_DIR/$expected_name.exe"
        # 非Windows平台检查常规文件
        elif [ "$OS" != "windows" ] && [ -f "$OUTPUT_DIR/$expected_name" ]; then
            echo -e "  - ${GREEN}$platform${NC}: $OUTPUT_DIR/$expected_name"
        else
            echo -e "  - ${RED}$platform${NC}: 未找到编译结果"
        fi
    done
else
    echo -e "\n${RED}[✗] 构建失败!${NC}"
    exit $BUILD_STATUS
fi
