# 检索输出与可选区域定位

状态：本地核心库与CLI的输出版本、bbox投影和分页预算修正已实现；核心全包test/vet/race及CLI test/vet通过，真实官方PDF compact实验也已完成。独立库与CLI保持 `doc-library/1` 默认；compare-host计划单独以v2作为宿主集成默认，两者默认值不互相改变。compare-host仍在其仓库联调，固定远程依赖尚未发布，本文不声称host接线或remote依赖已验收。

## 目标与边界

普通查询优先返回正文及可追溯出处。source/doc、PDF物理页或源码行范围始终保留；v2默认省略bbox，按需返回 `bbox_pct`。PDF页码既是引用信息，也是继续读页/看整页入口，不与区域坐标捆绑。

只提供一个区域定位选项 `include_bbox`，不另加 `locate`、`location_level`、`bbox_units`、`detail` 等近义参数。它控制响应中是否附加可用PDF区域，不改变来源身份、基本页/行定位或检索算法。

| 信息 | v2默认 | 说明 |
|---|---|---|
| source + doc | 保留 | 跨资料同名文件必须能区分 |
| PDF物理page / 源码start_line,end_line | 保留 | 支持引用、精读与看整页 |
| 正文 | 一份 | `results[].text` 为唯一命中正文来源，不复制顶层Text |
| bbox_pct | 省略，可选开启 | 可直接转换为 `view.region` |
| bbox点坐标 + page_width/height | 省略 | v2只在请求区域时输出bbox_pct；v1保持既有丰富结构 |
| cache namespace/fingerprint/hit/miss | 常规v2检索省略 | status/index等操作可保留所需状态 |
| 截断、提取失败、继续位置 | 保留 | 影响结果解释与下一步操作 |
| 纯输出选项 | 不影响持久缓存身份 | 不改变检索语义或提取缓存key |

源码没有PDF bbox，不生成假坐标。PDF结果中的 `start_line/end_line` 是页内段落块索引，不是TeX源行，展示时不能标成 `Lxx`。

## 版本与优先级

`output.version` 配置和请求 `output_version` 接受1或2。旧调用未配置仍默认为v1，以免静默改变已有CLI/库调用的响应；切换到v2必须明确配置或请求。CLI提供 `--output-version=1|2`，与程序版本查询 `--version` 不混淆。

`include_bbox` 支持可选布尔值。启动优先级为：JSON请求中的非null字段 > 已合并CLI覆盖的 `Config.Output` > 协议默认。CLI显式flag在创建库前写入config，因此JSON省略或 `null`（bbox）/`0`（version）继承当前启动默认；显式false或合法非零版本覆盖启动设置。CLI未显式传flag时，配置原值保留。

配置示例：

```yaml
output:
  version: 2
  include_bbox: false
```

CLI紧凑调用：

```bash
doc-library --config library.yaml --output-version=2 --mode search --query 'Dfacet Dscene3d'
```

按需请求PDF区域：

```bash
doc-library --config library.yaml --output-version=2 --include-bbox=true --mode search --query 'Dfacet Dscene3d'
```

裸 `--include-bbox` 等同true；false使用 `--include-bbox=false`。`--include-bbox` 只影响区域字段，不启用PDF解析或图片渲染，也不会改变 `include_image`、`view` 或 `region` 的行为。

## 响应结构

v2正常响应以Results作为正文唯一来源，示意：

```json
{
  "version":"doc-library/2",
  "ok":true,
  "mode":"search",
  "results":[
    {"source":"luadraw","doc":"doc/luadraw-doc-en.pdf","kind":"pdf","page":131,"text":"<命中正文>"}
  ],
  "images":[]
}
```

常规搜索/read不会镜像顶层 `text`，不附cache诊断；`status`/`index`保留操作所需的cache诊断。schema响应以 `oneOf` 描述v1与v2 wire shape。v2会过滤已知的常规binary-skip提示，其他重要warnings、truncated及next_offset仍保留。请求bbox后，PDF结果只增加 `bbox_pct`，不在v2同时重复bbox点坐标和页尺寸。PDF页内source block索引仍可在Go `Response.Results`中读取，但不序列化到v2 wire。Go `Response.Text`和`Response.Cache`字段仍保留给宿主使用；v2 `MarshalJSON`只输出一份Results正文，status/index除外仍有cache诊断。v1保持原丰富wire响应；显式 `include_bbox:false` 可投影掉既有区域字段。

## 实测

在一份真实官方PDF的三条同命中样例中，v1响应2091 bytes，v2为878 bytes，减少58.0%；三条 `results[].text` 正文完全相同。该比例仅描述这组样例，不外推为所有文档/查询的压缩率。`include_image` 分别搭配bbox false/true时，两种响应的图像尺寸都为444×92，命中内容一致；显式请求 `bbox_pct` 后按返回区域查看的图为348×24，已实际确认裁到目标目录文字。切换纯输出设置前后cache namespace相同。

正文预算 `max_chars` 仍只限制结果正文字符数，元数据另计；不声称其限制完整JSON字节数或token数。预算导致提前停止时，应依据实际消费的候选位置返回正确的 `next_offset`；snippet截断和结果集分页是不同状态。`next_offset` 表示可继续位置，不代表准确全库总命中数。

## 定位和图像

百分比采用左上原点xyxy（right/bottom是坐标，不是宽高）；继续看图可传：

```json
{"mode":"view","source":"luadraw","doc":"doc/luadraw-doc-en.pdf","page":131,"region":{"left":12.3,"top":18.4,"right":83.1,"bottom":42.7}}
```

示例页码和坐标仅用于格式说明，不表示特定调用实际命中。`include_bbox:false,include_image:true` 仍允许内部几何用于自动裁图；关闭坐标输出不代表不做定位提取。bbox缺失的frequent条目不会因为请求开关而凭空得到坐标，需要时应对已知source/doc/page使用对应读取/查看能力。

## 提取与缓存

PDF提取依赖 `pdftotext -bbox-layout`，段落分列、相邻命中合并和自动裁图使用其内部几何。关闭bbox输出是在完成内部检索、合并、频次记录或图片处理后控制响应投影；它减少响应字节和格式化噪声，不意味着免去PDF定位提取或明显加速搜索。

`output.version` 与 `include_bbox` 是纯展示设置，不进入提取cache key或资料namespace语义哈希。切换输出协议不应造成冷缓存、拆分高频历史或重索引。

## 集成状态与验证

本地doc-library Go API/CLI输出版本、请求/config/flag覆盖和分页语义已实现；CLI回归与vet通过，核心全包test/vet/race通过。独立库/CLI仍默认v1，配置或请求可选v2。compare-host的默认v2属于宿主自己的集成选择，不改变库默认；compare-host仍在另一仓库联调，固定远程依赖尚未发布，不能据本地实验声称其固定依赖或打包Skill已验证。

本地验收覆盖：v1旧响应形态；v2正文唯一、正常查询无cache、默认无bbox；bbox仅按需输出bbox_pct；JSON/JSONL省略、null、true、false及版本0继承；图片渲染与bbox投影独立；纯展示切换保持内容、排序、合并、frequency与cache身份；预算早停的next_offset及snippet截断/分页区分；read/view/list/find/frequent/status/index的wire投影。
