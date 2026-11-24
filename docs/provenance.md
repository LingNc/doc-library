# 复用来源

`doc-library` 从已有 `compare-figure` 的独立 `luadraw-doc` 文档能力抽取和泛化，不依赖绘图 runner、自动模型会话或 LuaDraw 运行时。

基线：`compare-figure` 提交 `820376cdc4158f8df7f3fcc61f678032f11bc6b9`。来源文件：

- `go/luadraw/docs.go`：Poppler PDF文字/bbox读取、几何段落合并、搜索和读页/裁图。
- `go/luadraw/doc_files.go`：库根路径约束、源码段落与行号、目录与文本查找。
- `go/luadraw/doc_frequency.go`：稳定知识片段标识、主动读取频次和可重建缓存。
- `go/app/docs_cli.go`：独立参数、JSON与逐行JSONL的交互约定。

原代码为MIT，Copyright (c) 2026 LingNc，完整许可证保留于根 `LICENSE`。

当前实现将可复用部分集中到 `pkg/doclibrary/`：`paragraphs.go` 保留原源码段落与PDF几何合并/去重，`pdf.go` 保留Poppler bbox/区域渲染并使用flow/block/line结构避免两栏串接，`engine.go` 以相同段落与命中定位组织七模式，`frequency.go` 维护稳定片段与主动访问频次。`library.go/cache.go` 泛化来源、授权范围、内容指纹和每文件缓存；平台锁文件使用宿主文件锁，进程退出释放持有权。CLI从旧接口约定提炼为独立的 `cmd/doc-library`，通过公开包调用，不桥接compare-figure可执行程序。

新工具的改造点是可配置多源根、结构化命中结果、过滤与预算、Unicode查询、独立缓存命名空间和懒加载。旧比较工具本轮保持原样；今后如将其改为调用本库，应通过公开API/CLI迁移，而不是在两个仓库分别维护通用改进。

PDF仍使用成熟Poppler工具，不新增PDF解析器、OCR或向量模型。文档内容和上游手册不复制进本仓库，所有权和许可证仍归各自项目；示例配置只是对外部目录的只读引用。
