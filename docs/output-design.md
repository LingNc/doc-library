# 检索输出与可选区域定位：设计提案

状态：设计提案，尚未实现。当前接口仍按 [调用契约](contract.md) 返回；本文示例中的 `output.include_bbox`、请求 `include_bbox` 与紧凑响应不是当前可用参数。此阶段不改 PDF 提取、不变更公开响应、不引入第二套索引。

## 目标与决定

普通查询优先让 Agent 读到内容及可追溯出处。默认显示 `source`、`doc`、PDF物理页或源码行范围、正文；默认不显示 bbox、页尺寸、缓存指纹。需要区域裁图或标注时，调用方单独请求 bbox。PDF页码保留：它既是引用信息，也是继续读页/看整页的入口，不必与区域坐标捆绑。

建议只增加一个定位输出选项 `include_bbox`，而不是同时增加 `locate`、`location_level`、`bbox_units`、`detail` 等近义参数。它控制是否附加可用的 PDF 区域，不改变来源身份、基本页/行定位或检索算法。

| 信息 | 建议默认 | 理由 |
|---|---|---|
| source + doc | 保留 | 跨资料同名文件必须能区分 |
| PDF物理page / 源码start_line,end_line | 保留 | 支持引用、精读与看整页，成本低 |
| 正文 | 一份 | 机器JSON不再同时附整份Text镜像 |
| bbox_pct | 默认省略，按需开启 | 可直接转换为view.region |
| bbox点坐标 + page_width/height | 紧凑输出不重复附加 | 同一矩形可由百分比与页尺寸换算；旧协议继续保留 |
| cache namespace/fingerprint/hit/miss、依赖revision | 不进入模型正文 | 属于诊断；保留在旧机器响应、status/index或宿主日志 |
| 真正的截断、提取失败、继续位置 | 保留 | 影响结果解释与下一步操作 |
| 正常跳过二进制等扫描细节 | 常规正文不反复附加 | 归诊断汇总；不能隐藏导致缺资料或提取失败的警告 |

源码没有 PDF bbox，不应为了响应开关生成假坐标。PDF结果中的 start_line/end_line 是页内段落块索引，不是 TeX源行，紧凑展示不标成 `Lxx`。

## 当前问题的实证

`types.go` 的 Response 同时含 Results 和 Text；`engine.go` 每次把结果再次格式化为 Text，CLI序列化两者。因此机器JSON的正文确实重复。compare-figure 适配层也保留这两份数据，但自动模型工具只收格式化 Text 与首张图，不能说模型又收了一份完整 Results。

PDF `bbox` 与 `bbox_pct` 不是两次计算出的不同位置，而是同一矩形的点坐标/百分比版本；compare-figure 的 formatter 将这两套数值、页尺寸一起塞进正文，还在第一行显示缓存hash等信息。坐标值不应成为普通内容检索的默认负担。

本次实际调用 `query=Dscene3d,limit=1,max_chars=800,snippet_chars=800` 的一个命中正文只有一行目录文字，但现有响应同时携带两套坐标、页尺寸、score/entry_id、缓存信息、provenance、Text镜像及重复warnings。该样本用来说明输出层膨胀，不据此对全部检索质量或排序下结论。

## 配置与单次覆盖

独立doc-library建议的启动配置：

```yaml
output:
  include_bbox: false
```

对应启动参数 `--include-bbox=true|false`。嵌入compare-figure时，runner配置入口为 `docs.output.include_bbox`，由宿主传给文档适配层；独立document-search的CLI同样可显式指定。两者都不扩大授权文档根。

工具请求：

```json
{"mode":"search","source":"luadraw","query":"Dfacet Dscene3d","limit":3}
```

需要区域：

```json
{"mode":"search","source":"luadraw","query":"Dfacet Dscene3d","limit":3,"include_bbox":true}
```

优先级：**本次请求显式true/false > 本次启动CLI覆盖 > 启动配置 > 当前输出协议默认**。新紧凑输出默认false；兼容v1的默认仍按旧契约。内部用可选布尔表达继承（如Go `*bool`），不能把显式false当成没传；配置开启后请求false必须能关闭。

三个开关/输入的职责不混合：

- `include_bbox`：是否返回区域坐标。
- `include_image`：search是否实际生成并附图；view本身就是显式看图。
- `region`：view裁剪输入，沿用页面百分比。

所以 `include_bbox:false,include_image:true` 仍可以用内部定位自动裁图；没有bbox输出时依然可以凭source/doc/page看整页。未缓存区域的frequent条目不会因为开关而凭空拥有bbox，需明确“附加可用区域”；如缺区域且调用确实需要，使用已有doc/page/blocks精读解析，不能静默重提取全库。

## 模型文字建议

默认：

```text
luadraw · doc/luadraw-doc-en.pdf · p.131
<命中正文>

luadraw-notes · luadraw-surface-rendering.md · L7–12
<命中正文>

内容已截断；可用 read 继续精读。
继续检索：offset=2
```

只有实际存在截断或可继续时才出现尾注。开启区域时PDF标题仅多一项：

```text
luadraw · doc/luadraw-doc-en.pdf · p.131
bbox_pct=[12.30,18.40,83.10,42.70]
<命中正文>
```

百分比采用左上原点xyxy（并非right/bottom宽高）；模型继续看图可传：

```json
{"mode":"view","source":"luadraw","doc":"doc/luadraw-doc-en.pdf","page":131,"region":{"left":12.3,"top":18.4,"right":83.1,"bottom":42.7}}
```

上面页码/区域是格式示意，不是声称本次实际命中了这些内容或位置。

## 紧凑机器响应建议

新契约以 Results 为唯一正文来源，宿主按需渲染可读文字，不在同一响应默认复制 Text。示意：

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

需要区域时在该结果中加 `bbox_pct`。仅在存在时加分页、截断与重要warnings；score、频次ID等机器用途字段按实际操作需要保留，不必显示在常规模型文字中。旧契约的点坐标和页尺寸不在新紧凑输出重复；高级程序如需绝对PDF测量，可继续使用旧丰富Response或显式PDF页信息API，先确认真实消费者需求再确定升级接口，不为假设用途堆开关。

## 分页与预算必须一起校正

当前 next_offset 主要依据“返回数量是否达到limit”推断，因此一方面只代表“可能还有”，另一方面正文预算先耗尽且结果数未达limit时，即便后面仍有候选，也可能漏续页入口。实现应记录实际候选消费位置和停止原因，而不是让formatter猜：

- `next_offset` 用于继续结果集，不代表精确总命中数。
- 单条snippet截短与结果集还有后续是独立信息。snippet截断可引导read，分页才用offset。
- 如果还有候选，预算导致停止也应给正确的继续位置；不能因重复截断在同一个命中上循环。
- `max_chars` 当前是正文字符预算，元数据另计；先保留清楚语义，不声称它限制全部JSON大小或token数。后续若要严格限制模型完整文本，应由适配层预留出处与重要尾注预算，不截掉警告来凑限额。

## 提取与缓存边界

当前PDF提取固定使用 `pdftotext -bbox-layout`，段落分列、相邻命中合并、自动裁图依赖几何数据。`include_bbox=false` 应在检索/合并/频次记录/附图之后执行响应投影，不能提前删除内部几何。它减少响应字节、模型上下文与格式化噪声，**不代表免去PDF定位提取或明显加速搜索**。

纯输出配置不进入提取cache key，也不进入资料namespace的语义哈希。现有库对整个Config做namespace哈希，新增output字段时需显式剥离这一展示设置，避免换一个显示开关就造成冷缓存、拆分高频历史或重索引。

## 迁移顺序

1. 先让compare-figure模型可读文字去掉缓存首行与双坐标，保留出处和重要warnings；此层变化不必改机器JSON协议。
2. 在doc-library补输出投影与可选布尔覆盖，schema、CLI、JSON/JSONL及宿主默认接线同时设计。不要只加字段但启动配置实际不生效。
3. 当前 `doc-library/1` 明确承诺PDF定位字段，cache也是既有响应成员。不能静默删除或把省略include_bbox的默认反转。兼容期v1保留既有行为，可显式false缩减；紧凑默认及去Text镜像在明确的新契约中一次落地。不要另加一组detail/debug/legacy近义开关。
4. compare-figure使用固定远程doc-library依赖。核心实现后需先验证上游，再更新固定revision、适配层及导出包；不能用工作区相邻replace制造本地成功、安装包失效。

本提案没有启用新协议、没有实现以上参数，也没有更新固定依赖。后续实现应保留原响应样本进行兼容验证。

## 验收标准

- 默认PDF结果只有页定位，源码行号仍准确；默认模型文字无缓存hash、bbox或页内block伪行号。
- 启动true/false与单次true/false/省略组合覆盖正确，尤其显式false能压过true默认。
- 开关不改变命中内容、排序、合并、frequency及cache身份；开关bbox不会导致重新提取。
- bbox开启仅附一种坐标，view按该区域裁到同一内容；关闭bbox仍能自动附图/看整页。
- 默认JSON正文一份；模型格式与JSON使用同一个投影规则但不同序列化出口，未产生第三份重复正文。
- 预算早停、恰好limit、snippet截断、最后一页、空结果都能正确继续或结束，不循环、不虚构total。
- read/view/list/find/frequent不误套PDF搜索格式，缺区域如实显示；跨source同名文件无歧义。
- v1旧调用、v2显式调用及迁移后的固定依赖/可移动Skill包都真实验证。
