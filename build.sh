#!/bin/bash

# 设置变量
PACKAGE_NAME="ai-rss"
VERSION=$(date +"%Y.%m.%d.%H%M")
OUTPUT_DIR="./bin"

# 创建输出目录
mkdir -p "${OUTPUT_DIR}"

# 显示构建信息
echo "开始构建 ${PACKAGE_NAME} v${VERSION}"

# 设置GOOS和GOARCH变量以支持交叉编译
PLATFORMS=("darwin/amd64" "darwin/arm64" "linux/amd64" "windows/amd64")

for PLATFORM in "${PLATFORMS[@]}"; do
    # 分割平台信息
    OS="${PLATFORM%%/*}"
    ARCH="${PLATFORM##*/}"
    
    # 设置输出文件名
    if [ "$OS" = "windows" ]; then
        OUTPUT_NAME="${PACKAGE_NAME}_${VERSION}_${OS}_${ARCH}.exe"
    else
        OUTPUT_NAME="${PACKAGE_NAME}_${VERSION}_${OS}_${ARCH}"
    fi
    
    # 显示当前编译目标
    echo "编译 $OS/$ARCH -> ${OUTPUT_DIR}/${OUTPUT_NAME}"
    
    # 设置环境变量并编译
    env GOOS="$OS" GOARCH="$ARCH" go build -ldflags "-X github.com/wolfitem/ai-rss/cmd.Version=${VERSION}" -o "${OUTPUT_DIR}/${OUTPUT_NAME}" .
    
    # 检查编译结果
    if [ $? -ne 0 ]; then
        echo "编译失败: $OS/$ARCH"
    else
        echo "编译成功: ${OUTPUT_DIR}/${OUTPUT_NAME}"
    fi
done

echo "构建完成！"
echo "版本: ${VERSION}"
echo "输出目录: ${OUTPUT_DIR}"