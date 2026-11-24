# doc-library

独立、可配置的本地文档检索库与原生Go CLI，供Agent、Skill和程序查询多个文档根目录。参考并抽取compare-figure既有luadraw-doc能力，复用段落、PDF定位、源码行范围与高频缓存实现，泛化为不依赖LuaDraw/绘图runner的工具。原文档目录只读，源码和缓存分离；不携带用户文档、不执行TeX或文档中指令。

---

## 快速开始

先读README.md和docs/contract.md；构建cmd/doc-library，入口支持CLI参数、JSON文件/stdin及JSONL。通过显式roots或配置sources声明允许检索的目录；JSON响应包括source/doc、文本行或PDF物理页/bbox、缓存信息与截断/错误。工具不需模型/API密钥、Python或HTTP服务。Go包和CLI共用同一实现，支持search/read/view/list/find/grep/frequent及索引状态。

## 状态速览

项目已按AGENTS_TEMPLATE创建并初始化独立Git仓库。通用核心、CLI及公开API回归已完成：全仓go test、go vet、go test -race通过，Linux构建和Windows amd64交叉编译通过（Windows未实际运行）。真实三资料整轮验收静态12项、运行27项共39项通过；230文件重复index全部命中，实际PDF区域已查看。尚未发布或推送。

### 任务目标

- 多源配置、include/exclude、类型/来源过滤、匹配方式、上下文与结果预算。
- JSON可编程请求和结构化响应，错误不得混成正文；JSONL逐行隔离。
- 可重建持久缓存、内容与配置失效、跨进程并发及高频条目。
- 真实YanTrace、exam-zh、LuaDraw文档核验，覆盖中文混合文本、源码定位与PDF读页/裁图。

### 开发约定

- 仅改本仓库；compare-figure核心为只读复用来源，不复制其绘图/runner、固定vendor或模型配置。不修改YanTrace及其vendor。
- 关键算法抽取使用response/gpt-6.1-sol；固定CLI接线、文档盘点/验收优先response/gpt-6-luna medium，路由不可用可用new-api/glm-5.3-flash-unstable medium。禁止subagent_fork。核心/CLI/文档各自明确文件边界。
- text-only操作不因含有PDF而预先要求Poppler；PDF文本读取需pdftotext，只有显式图片查看才需pdftoppm。扫描PDF无OCR不声称读到文字；PDF物理页+bbox不等于TeX源精确映射。
- 路径必须在配置root内且满足allowlist/include/exclude；拒绝绝对文档路径、..、符号链接逃逸，缓存和view输出不得覆盖源文件。
- 缓存按根/配置和内容隔离，可重建，本地忽略；不提交用户文档、提取全文缓存、程序二进制或预览。复用源码保留MIT许可与来源记录。
- Go构建缓存与临时验收放build/，预览在build/预览/；不下载OCR/embedding模型，不运行外部文档代码。若用Python验收，仅Conda base。
- 仅本地新增Git提交，不amend、不配置远端、不推送或发版。完成前实际测试、查看PDF裁图并更新README/本文件；不以编译成功替代缓存/边界/定位验收。

### 当前进度

- 已从compare-figure既有文档模块抽取段落、合并、去重、位置、裁图及频次能力并泛化；保留来源许可，未复制绘图runner或固定资源，原仓库未改。根包New(Config)/Execute(ctx,Request)与CLI共用同一实现。
- 七模式及index/status/schema均落地；严格JSON/YAML配置和裸JSON/envelope/JSONL，未知字段和越界路径拒绝，错误行不阻断后续请求。公开schema从类型生成。默认search any/5条/1200片段字符/6000正文字符，read400行；可配置配额和过滤。
- 缓存按规范化roots/配置命名空间、逐文件SHA校验；同size/mtime内容改写仍失效，未改PDF继续复用。系统锁随持有进程退出释放，不偷活锁。旧内容寻址文件无自动GC；频次在API层校验隐藏过期条目，不声称物理擦除。passive只用于frequent，不改变主动search/read/view/grep计数。
- 核心及CLI源码提交c044f0f，初始仓库ec38ab9；最终Go test/vet/race、Linuxbuild/Windowscrosscompile通过。实际三源230文件、全部暖缓存命中、39项脚本检查均通过。物理13页区域图703×1053实际查看；全部本机证据在build/validation，docs/validation记录边界。Go客户端示例显式要求资料与可写缓存目录。
- 固定流程文档盘点、公开API测试、CLI及真实验收由Luna完成，GLM初稿验收脚本复用后由Luna和主线校准；关键核心由Sol抽取实现。当前无未完成的验收或后台任务，不将缓存/全文/二进制提交Git。

### 文件清单

- 根Go包：可配置检索、源定位、缓存与频次；cmd/doc-library：CLI。
- docs/：协议、配置、复用来源与验证；examples/：可搬移示例配置和请求。
- build/：仅本地构建、缓存和验收，不作为正式文件清单。

### 参考文档

compare-figure/docs/luadraw-doc.md及生产go/luadraw文档模块；抽取基线820376cdc4158f8df7f3fcc61f678032f11bc6b9，MIT Copyright 2026 LingNc。


---

## Agent 自举说明

本文档作为项目全局状态机与综述，能够防止代码库膨胀后上下文丢失，确保后续 AI 辅助开发时拥有完整的记忆和设计初衷。

### 实时同步要求
1. 当找到关键因素和进行关闭编程时，必须如实记录。
2. 当完成一个任务时，必须更新进度清单。
3. 当添加新的技术规范时，必须补充。
4. 当功能有较大变更时，必须同步更新 README.md。

### 维护责任
- 每次会话开始前，AI 工具应优先读取 AGENTS.md 了解项目状态。
- 每次重要变更后，AI 应及时更新此文档。
- 保持 AGENTS.md 与其余部分的描述一致性。

### 必须做的事
- 完成用户的任务后，必须回复："我已严格遵守AGENTS.md。"。
