# doc-library

把本地手册、源码和项目说明接成一个可检索文档库。通过 YAML 配置指定多个文档目录，用命令行、JSON 或 JSONL 查询；结果返回来源、文件、文本行号，或 PDF 物理页和区域，方便继续精读与引用。

核心为独立 Go 库，CLI 调用同一实现。复用并泛化 compare-figure 的段落读取、PDF 定位和高频片段能力，不要求绘图会话、模型密钥或上传文档。原始文档内容只读，索引和使用频次存入配置的缓存目录。

提供 `search/read/view/list/find/grep/frequent`，另有 `index/status/schema`。支持多来源、类型过滤、正则、匹配方式、结果数量和正文预算；响应带定位与截断信息，显式查看时才生成图像。独立Go库/CLI支持v1兼容默认和显式v2紧凑wire响应，bbox按需输出；本地API、CLI和真实PDF实验已验证。compare-host计划单独默认v2，仍在其仓库联调，固定远程依赖尚未发布。

## 快速开始

```bash
# Go 1.25+；在仓库根构建，不需要 Python 或模型密钥。
GOCACHE="$PWD/build/go-cache" go build -o build/doc-library ./cmd/doc-library

# 最小自检：只检索本仓库说明，无需外部资料。
build/doc-library --config examples/config/minimal.yaml --mode search --query cache_dir

# 多资料：按自己的目录修改配置中的 sources。
build/doc-library --config examples/config/libraries.yaml --mode search --source yantrace --kind source --query LRSheetCompose

# JSONL 是查询请求，配置仍由 YAML 加载。
build/doc-library --config examples/config/libraries.yaml --jsonl < examples/requests/search.jsonl
build/doc-library --mode schema
```

也可用 `--root manuals=/path/to/docs --cache-dir build/doc-cache` 直接指定资料，无需配置文件。配置内的相对路径以配置文件所在目录为基准。

## 配置与请求

- **YAML 配置**：声明允许访问哪些文档根目录、过滤规则、缓存位置及默认预算。正式示例统一放在 [examples/config/](examples/config/)，从 [minimal.yaml](examples/config/minimal.yaml) 或 [libraries.yaml](examples/config/libraries.yaml) 开始。个人配置建议命名为 `*.local.yaml`，Git 已忽略。
- **JSON/JSONL 请求**：指定这次搜索什么、读取哪一页、返回几条；不负责授权新的资料目录。[search.jsonl](examples/requests/search.jsonl) 一行一个请求，配合 `--jsonl` 使用。单次 JSON 请求也可通过 `--stdin` 或 `--request` 提交。
- **JSON 响应与缓存**：响应是供程序解析的结构化结果；缓存中的 JSON 是可重建的内部数据，不是需要手工维护的配置。

程序仍兼容 JSON 配置，已有调用无需迁移；本仓库的配置示例统一使用 YAML。Go 程序可直接传入 `Config`，无需配置文件，见 [Go 调用示例](examples/go-client/main.go)。

## 目录

```text
cmd/doc-library/       CLI 入口与 CLI 测试
pkg/doclibrary/        核心 Go 库、公开类型、单元及集成测试
examples/config/      YAML 配置示例
examples/requests/    JSONL 请求示例
examples/go-client/   Go API 调用示例
docs/                 调用契约、Agent 用法、开发与验收说明
scripts/              开发期验收脚本
licenses/             第三方许可证
testdata/             测试资料约定
build/                本机构建、缓存、验收报告和预览（不入 Git）
```

Go 包导入路径为 `doc-library/pkg/doclibrary`，包名 `doclibrary`。当前为本地独立项目，尚未发布；跨项目使用源码或本地 `replace`，远程 Go 模块路径需在发布时确定。

## 文档与边界

- [调用契约](docs/contract.md)：配置、CLI、JSON/JSONL、响应和错误。
- [Agent 用法](docs/agent-usage.md)：检索、定位、精读和引用。
- [开发说明](docs/development.md)：构建、回归和目录维护。
- [验证记录](docs/validation.md)：真实 YanTrace、exam-zh、LuaDraw 文档及缓存验证。
- [缓存性能研究](docs/cache-design.md)：文件与 SQLite 双后端、FUSE/NFS 约束及后续优化建议（设计草案，尚未实现）。
- [检索输出设计](docs/output-design.md)：本地v1/v2协议、bbox控制、分页/缓存边界及真实PDF实验记录；compare-host联调状态单独说明。
- [复用来源](docs/provenance.md)：抽取基线、许可证和独立边界。

普通文本操作无需 PDF 工具；PDF 文本依赖 Poppler 的 `pdftotext`，显式看图另需 `pdftoppm`。不做 OCR，不执行文档中的 TeX/Lua 代码；PDF 页码/区域定位不代表精确反查源代码行。内容寻址缓存目前没有自动垃圾回收，旧提取数据可能继续占用磁盘。

自有及抽取源码遵循 [MIT 许可证](LICENSE)，依赖许可见 [第三方组件](docs/third-party.md)。本地文档、索引、预览与构建产物不随源码提交。
