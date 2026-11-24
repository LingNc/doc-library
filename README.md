# doc-library

把本地手册、源码和项目说明接成一个可检索文档库。通过配置指定多个文档目录，用命令行、JSON或JSONL查询；命中结果返回来源、文件及文本行号，或PDF物理页和区域，方便Agent继续精读与引用。

复用并泛化compare-figure已有文档工具的段落读取、PDF定位和高频片段能力，不要求绘图会话、模型密钥或上传文档。核心为Go库，CLI调用同一实现。原始文档只读，索引和使用频次缓存在可写目录；新增、修改或删除文档后按内容和配置更新缓存。

提供 `search/read/view/list/find/grep/frequent`，另有 `index/status/schema` 用于初始化、检查和接口发现。可按来源、文件类型、扩展名、匹配方式、大小写、正则、结果数量和正文预算收窄查询。结果带结构化定位、缓存命中与截断信息；普通查询默认只返回文字，显式查看时才生成图像。

```bash
# Go 1.25+；本机构建一次，不需要Python或模型密钥。
go build -o build/doc-library ./cmd/doc-library

# 一个目录即可使用，也可用配置组合多个来源。
build/doc-library --root manuals=/path/to/docs --cache-dir build/doc-cache --mode search --query LRSheetCompose
build/doc-library --config examples/libraries.yaml --mode search --source yantrace --kind source --query LRSheetCompose

# 请求与响应均为JSON；JSONL每行单独执行。
build/doc-library --config examples/libraries.yaml --jsonl < examples/requests.jsonl
build/doc-library --mode schema
```

[最小JSON配置](examples/minimal.json)只检索本仓库说明文档，可直接用 `--config examples/minimal.json --mode search --query cache_dir` 自检。[多资料YAML配置](examples/libraries.yaml)接入本工作区的YanTrace、exam-zh和LuaDraw，路径相对配置文件；换环境时改为自己的资料目录即可。[Go调用示例](examples/go-client/main.go)演示程序通过 `New(Config)` / `Execute(ctx, Request)` 直接复用同一核心，显式传入资料目录与可写缓存目录。

- [调用契约](docs/contract.md)：配置、多来源、命令参数、JSON/JSONL、响应和错误。
- [Agent用法](docs/agent-usage.md)：检索、定位、精读和引用的顺序。
- [复用来源](docs/provenance.md)：抽取基线、许可证和独立边界。
- [验证记录](docs/validation.md)：真实YanTrace、exam-zh、LuaDraw文档及缓存验证。
- [开发说明](docs/development.md)：构建、回归、匿名fixture和维护边界。

普通文本操作无需PDF工具。PDF文本依赖Poppler的pdftotext，显式看图才需要pdftoppm；本工具不做OCR，也不执行文档中的TeX/Lua代码。PDF页码/区域定位不代表能够精确反查该页对应的源代码行。

自有及抽取源码遵循[MIT许可证](LICENSE)，依赖许可见[第三方组件](docs/third-party.md)。本地文档、索引、预览与构建产物不随源码提交。
