# AI-RSS 新闻聚合与分析工具

这是一个基于Go语言的控制台程序，用于获取指定OPML文件中订阅的RSS源，拉取每个RSS地址的文章内容，并使用Deepseek API对文章内容进行提炼总结，最终输出每日新闻报告。

## 功能特点

- 读取OPML文件，提取RSS订阅源
- 下载并解析RSS源中的XML内容
- 筛选最近几天发布的文章
- 调用Deepseek API进行内容分析和总结
- 以Markdown表格形式输出新闻总结报告

## 安装方法

### 从源码构建

```bash
# 克隆仓库
git clone https://github.com/wolfitem/AI-RSS.git
cd ai-rss

# 给构建脚本添加执行权限
chmod +x build.sh

# 使用构建脚本编译
./build.sh
```

构建脚本会自动为以下平台生成可执行文件：
- macOS (Intel, M1/M2/M3)
- Linux (x86_64)
- Windows (x86_64)

编译完成后，可执行文件将保存在`bin`目录下，文件名格式为：`ai-rss_版本号_操作系统_架构`。例如：
```
bin/
├── ai-rss_2025.03.14.1640_darwin_amd64    # macOS Intel版本
├── ai-rss_2025.03.14.1640_darwin_arm64    # macOS Apple Silicon版本
├── ai-rss_2025.03.14.1640_linux_amd64     # Linux x86_64版本
└── ai-rss_2025.03.14.1640_windows_amd64.exe  # Windows x86_64版本
```

### 直接下载二进制文件

您可以从[Releases页面](https://github.com/wolfitem/ai-rss/releases)下载最新的预编译二进制文件。

## 使用方法

1. 将 config.example.yaml  重全名成 config.yaml  
2. 在 config.yaml 中配置 Deepseek API 密钥和 OPML 文件路径
3. 按不同系统的工具版本使用对应的方式运行 

### 不同系统版本的使用方式

#### macOS (Intel 或 Apple Silicon)

```bash
# 直接运行对应架构的可执行文件
./bin/ai-rss_版本号_darwin_amd64 process
# 或者 Apple Silicon 芯片的 Mac
./bin/ai-rss_版本号_darwin_arm64 process

# 如果想要更方便地使用，可以将可执行文件复制到 /usr/local/bin 目录下
sudo cp ./bin/ai-rss_版本号_darwin_arm64 /usr/local/bin/ai-rss
chmod +x /usr/local/bin/ai-rss
# 然后就可以直接使用 ai-rss 命令
ai-rss process
```

#### Linux (x86_64)

```bash
# 直接运行可执行文件
./bin/ai-rss_版本号_linux_amd64 process

# 或者添加到系统路径
sudo cp ./bin/ai-rss_版本号_linux_amd64 /usr/local/bin/ai-rss
chmod +x /usr/local/bin/ai-rss
# 然后就可以直接使用 ai-rss 命令
ai-rss process
```

#### Windows (x86_64)

```bash
# 在命令提示符(CMD)中运行
bin\ai-rss_版本号_windows_amd64.exe process

# 在PowerShell中运行
.\bin\ai-rss_版本号_windows_amd64.exe process
```

### 基本用法

```bash
# 使用默认配置文件处理RSS
ai-rss process
# 指定配置文件
ai-rss process --config=custom_config.yaml
```

### 命令说明

- `process`: 处理OPML文件并生成新闻报告
  - `--config, -c`: 指定配置文件路径（可选，默认为./config.yaml）

- `version`: 显示程序版本信息

## 配置文件

默认配置文件为`config.yaml`，您可以通过`--config`参数指定自定义配置文件。配置文件示例：

```yaml
# 日志配置
logger:
  level: "debug"           # 日志级别: debug, info, warn, error, dpanic, panic, fatal
  console: true            # 是否输出到控制台
  file_path: "logs/ai-rss.log" # 日志文件路径
  max_size: 100            # 单个日志文件最大大小，单位MB
  max_backups: 3           # 最多保留的旧日志文件数量
  max_age: 28              # 保留日志文件的最大天数
  compress: true           # 是否压缩旧日志文件

# Deepseek API配置
deepseek:
  api_key: "your_api_key_here"
  model: "deepseek-chat"
  max_tokens: 2000
  max_calls: 300           # API最大调用次数，超过此次数将直接返回原始数据

# RSS获取配置
rss:
  days_back: 3             # 获取几天内的文章，默认为3天
  opml_file: "example.opml"   # OPML文件路径

```

## 示例输出

```markdown
# 新闻摘要报告 (2023-04-01 至 2023-04-03)

| 标题 | 内容摘要 | 来源 | 发布时间 | 分类 |
|------|---------|------|----------|------|
| 科技巨头发布新AI模型 | 该公司发布了新一代AI模型，性能较前代提升30%，主要改进了上下文理解能力和多语言支持。 | TechNews | 2023-04-02 | 科技新闻 |
| 全球气候变化会议召开 | 来自195个国家的代表齐聚一堂，讨论减少碳排放的新措施。会议强调了立即行动的必要性。 | WorldNews | 2023-04-01 | 国际新闻 |
```

## 许可证

本项目采用MIT许可证。详见[LICENSE](LICENSE)文件。

## 贡献指南

欢迎提交问题和拉取请求。对于重大更改，请先开issue讨论您想要更改的内容。