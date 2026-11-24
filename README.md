# doc-library

把本地手册、源码和项目说明接成一个可检索文档库。通过配置指定多个文档目录，用命令行、JSON或JSONL查询；命中结果返回来源、文件及文本行号，或PDF物理页和区域，方便Agent继续精读与引用。

复用并泛化compare-figure已有文档工具的段落读取、PDF定位和高频片段能力，不要求绘图会话、模型密钥或上传文档。核心为Go库，CLI调用同一实现。原始文档只读，索引和使用频次缓存在可写目录；新增、修改或删除文档后按内容和配置更新缓存。

当前正在实现，正式CLI/JSON契约与验收结果将在完成后写入下列文档：

- [调用契约](docs/contract.md)：配置、多来源、命令参数、JSON/JSONL、响应和错误。
- [复用来源](docs/provenance.md)：抽取基线、许可证和独立边界。
- [验证记录](docs/validation.md)：真实YanTrace、exam-zh、LuaDraw文档及缓存验证。

普通文本操作无需PDF工具。PDF文本依赖Poppler的pdftotext，显式看图才需要pdftoppm；本工具不做OCR，也不执行文档中的TeX/Lua代码。PDF页码/区域定位不代表能够精确反查该页对应的源代码行。

源码遵循[MIT许可证](LICENSE)。本地文档、索引、预览与构建产物不随源码提交。
