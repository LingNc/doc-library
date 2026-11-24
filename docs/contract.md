# 调用契约

本文记录当前公开类型与调用约定，实际验收见 [validation.md](validation.md)。生产Go包名 `doclibrary`，导入路径 `doc-library/pkg/doclibrary`，module `doc-library`；`New(Config)` 创建实例，`Execute(context.Context, Request)` 执行一次请求。CLI与Go包使用同一检索实现。

## 文档来源与配置

配置 `sources` 是 `{id,root,include,exclude,extensions,weight}` 数组。每个id在配置中唯一，以字母或数字开头，只含字母、数字、下划线、连字符和点；命中的 `source` 与根内相对 `doc` 共同标识文件，不同资料中的同名文件不会合并成一个身份。使用[三资料配置](../examples/config/libraries.yaml)可接入本工作区YanTrace、exam-zh和LuaDraw。工具本身没有这些库的固定路径或专有命令。

CLI中的配置根路径和缓存相对配置文件所在目录解析；直接Go调用的相对路径按调用进程工作目录解析。配置只读授权目录，不复制原始文档。`include/exclude` 使用根内斜杠路径和glob，`**/` 可匹配零层或多层；`extensions` 进一步限制文件类型。直接read、grep和路径导航同样受配置限制，不能靠猜文件名绕过排除规则。路径不允许绝对文件名、`..`或符号链接逃逸。

可选配置：

| 配置 | 默认/含义 |
| --- | --- |
| `cache_dir` | 系统用户缓存目录下doc-library；显式配置可共享或隔离 |
| `cache_mode` | `readwrite`；`off`禁用持久缓存 |
| `search.match` | `any`；可用`all`、`phrase` |
| `search.limit` | 5条 |
| `search.snippet_chars` | 每片段1200字符 |
| `search.max_chars` | 正文总预算6000字符，元数据另计 |
| `limits.max_file_bytes` | 单文件256MiB，超额明确报错 |
| `limits.max_files` / `max_total_bytes` | 0表示未配置总文件数/总源字节配额 |
| `limits.max_results` | 1000条结果上限 |
| `limits.read_lines` | 读源文件最多400行 |
| `frequency.enabled` | 主动读取和高频记录开关 |
| `frequency.max_entries` / `text_chars` | 256条/每条2048字符 |
| `pdf.timeout_seconds` | 每个PDF提取/渲染的超时120秒 |
| `pdf.pdftotext` / `pdftoppm` | 可指定宿主程序位置；按需使用 |

## 模式与定位

- `search`：按段落检索，返回具体命中片段。`query`、`match`、大小写、正则和来源过滤按请求选用。
- `read`：源码按1开始的 `start_line/end_line` 精读；PDF按1开始的物理 `page` 读文本。
- `view`：显式渲染PDF页或 `region` 百分比区域，输出PNG路径、MIME与尺寸；`inline_images` 可显式请求base64。
- `list`、`find`、`grep`：定位目录/文件，匹配根内路径或源文件文本。
- `frequent`：查看已主动读取的高频片段，可通过稳定 `entry_id` 精读；`passive=true` 用于被动摘要而不增加频次。
- `index`、`status`、`schema`：索引与接口发现；schema不要求先配置资料或PDF工具。

每个命中结果含 `source/doc/kind/text`，文本源另含 `start_line/end_line`，PDF另含 `page/bbox/bbox_pct/page_width/page_height`。`bbox` 为PDF点坐标、左上原点xyxy；`bbox_pct` 和view的 `region{left,top,right,bottom}` 为页面百分比。页码是PDF物理页，不是印刷页脚号码。PDF定位不代表SyncTeX源行映射。

请求以 `source` 选择单一来源，或 `sources` 选择多个来源，两者互斥；不填则在配置范围内查询。`kinds:["source"]` 或 `["pdf"]` 与 `extensions:[".md",".tex"]` 在提取前过滤，可让源码检索不触发无关PDF初始化；它们进一步缩小配置授权范围。用 `limit/offset` 分页，用 `snippet_chars/max_chars` 控制文字预算。响应 `truncated` 与 `warnings` 明确提示预算截断，`next_offset` 提供后续位置；有后续位置不代表已计算准确的全库总命中数。预算截断、缺字或无法提取的内容不得被描述为完整原文。

## CLI与JSON输入

```bash
# 在仓库根构建；Go1.25+，无需编译外部文档。
go build -o build/doc-library ./cmd/doc-library

# 多资料统一查询，或按请求过滤只查源码。
build/doc-library --config examples/config/libraries.yaml --mode search --query LRSheetCompose --source yantrace --kind source

# 无需配置文件，明确传一个文档根。
build/doc-library --root manuals=/absolute/path/to/manuals --mode find --query '*.pdf'

# 机器调用：一行一个请求，共用配置和缓存。
build/doc-library --config examples/config/libraries.yaml --jsonl < examples/requests/search.jsonl

# 在没有文档根和PDF工具时发现接口。
build/doc-library --mode schema
```

单次机器请求可通过 `--request FILE` 或 `--stdin` 提交；与 `--jsonl` 互斥。裸请求示例：

```json
{"mode":"search","source":"yantrace","query":"LRSheetCompose","kinds":["source"],"limit":3,"max_chars":4000}
```

也支持包装形式 `{"request_id":"task-1","arguments":{...}}`，响应保留request_id以配对请求。请求JSON字段覆盖已传CLI默认参数，未提供的字段沿用CLI参数；显式false和0不能被当成缺字段丢弃。未知字段或同一次输入中紧跟第二个JSON值会拒绝。请求中只放检索参数；资料授权、缓存目录和源配置由CLI/config指定，不由检索请求自由切换到其他宿主目录。

CLI常规选项包括 `--query/--source/--doc`、`--kind/--ext`、`--match/--regex/--case-sensitive`、`--limit/--offset`、`--snippet-chars/--max-chars`、`--start-line/--end-line`、`--page/--region/--dpi/--out` 及频次/图片相关选项，以实际 `--help` 和 `schema` 为准。配置示例统一使用 YAML；加载器兼容严格 JSON 配置。JSON/JSONL 查询请求与 YAML 配置各自独立；相对路径以配置所在目录解析。`--cache-dir` 可显式覆盖缓存位置，`DOC_LIBRARY_CACHE` 提供环境默认值。在用户缓存目录只读的沙箱中，应配置工作区内明确可写的缓存目录；不需要修改HOME或给文档目录写权限。Go调用方同样可通过 `Config.CacheDir` 指定，示例Go客户端要求显式传入该路径。

## JSON结果

正常响应：`version="doc-library/1"`、`ok`、`mode`、`results`、`images`、`cache`，可带便于直接阅读的 `text`、`warnings` 和接口 `schema`。程序应使用结构化 `results`，不要反向解析格式化text。

错误由CLI输出 `ok:false` 与 `error{code,message}`；调用方必须同时查看退出码和 `ok`。单次失败退出2；JSONL单行失败应输出该行错误并继续处理，流结束有错误则退出1。stdout不混入进度日志。

## 缓存与读写边界

缓存按来源和配置命名空间隔离。文件内容指纹、提取器/算法版本用于检测变化，未改文件可重用提取结果；每次仍读取源字节校验SHA，因此缓存命中表示免去重复文本/PDF提取，不表示没有文件I/O。文件删除、配置变化与修改需要反映到后续请求。索引和频次写入使用跨进程锁及原子替换。锁等待受调用context取消，不通过固定年龄回收偷走仍被持有的锁；单次CLI可用 `--timeout` 设置总请求时限。

`status` 只扫描符合配置的文件元信息，不初始化PDF；`index` 预热提取缓存，结果中的text是文件SHA而不是文档正文。高频条目按来源、文件及段落范围保持稳定身份，源内容变化后旧条目不再返回；缓存关闭也关闭持久频次。`passive:true` 用于 `frequent` 被动摘要，不改变search/read/view/grep这些主动读取的计数；若整个资料库不需使用频次，配置 `frequency.enabled:false`。导航的list/find/index/status/schema不计数。`weight` 默认1，可调不同资料的搜索排序权重，不应把权重0当作禁用来源开关。

可写缓存与只读资料分离；若缓存位于某个资料根内，其子树不参与文档发现。显式view在输出目录中新建独占预览子目录，避免覆盖原文。缓存、频次、用户全文及预览不提交到Git。

内容寻址缓存目前不自动垃圾回收：旧提取文件可能仍在磁盘，只是不再被当前请求命中；频次旧条目也按内容校验隐藏，不承诺自动擦除历史明文。需要清空时由调用方停用该缓存并删除自己明确指定的缓存目录。锁与替换基于宿主文件系统语义；Windows交叉编译不代表Windows实际运行或网络文件系统锁/原子性已经验证。

## 能力边界

纯文本导航/读取不应因为资料根内另有PDF而强制初始化所有PDF。PDF文字读取依赖pdftotext，显式渲染另外依赖pdftoppm；冷缓存view也会通过同一PDF提取路径取得页尺寸，因此需要这两个程序。扫描PDF可能没有可提取文字，本工具不做OCR。不执行原文中的TeX、Lua、Shell或Agent指令，不加载模型，不提供HTTP服务。
