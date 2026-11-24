# doc-library 真实资料验收

## 范围

验收脚本：[scripts/validate_libraries.py](../scripts/validate_libraries.py)，仅开发期通过 subprocess 调用 Go CLI；生产实现不经过 Python。配置为 [examples/libraries.yaml](../examples/libraries.yaml)，YanTrace、exam-zh 和 LuaDraw 根目录只读，不复制资料。报告、缓存、日志和预览保存在忽略目录 `build/validation/`、`build/doc-cache/`。

## 复现

使用 Conda base Python 和最终 CLI；绝对 CLI/config 路径从 `/tmp` 调用，检查配置相对路径不受 cwd 影响：

```bash
/home/lingnc/miniconda3/bin/python scripts/validate_libraries.py --runtime --pdf-page 13
```

## 结果

固定报告：[build/validation/report.json](../build/validation/report.json)。报告区分静态预检与运行检查，并记录 CLI SHA-256、配置 SHA-256、仓库 revision。最终修正脚本已完整运行并以退出码0结束：静态12项、运行27项，共39项通过，0 WARN、0 FAIL。

## 命中与缓存

YanTrace 的 `LRSheetCompose` 命中 `doc/src/chapters/06-answer-card.tex:17`，并按该命中 read 回原文。查询 `中括号 LRCompactAnswerCard` 与 `题号 作答框` 各命中 2 条。exam-zh 的 `choices`、`fillin` 源码查询各命中 8 条。LuaDraw 的 `Dpolyline3d` 命中 `doc/src/body-en/luadraw-doc-en-Ext-compile-tex.tex:119`，`Dscene3d` 命中 `doc/src/body-en/luadraw-doc-en-Ext-linprog.tex:228`；命中均验证来源 ID、根内文件存在和正行号。

全库状态扫描发现230个文件。最终整轮验收中的两次index均为230 hits、0 misses；这属于已预热缓存的复用验证。零miss表示缓存提取复用，不表示零源I/O，库仍读取源文件计算SHA。两次被动frequent请求均成功且频次缓存mtime_ns不变。单文件内容变化、同大小同时间戳变更和其余文件继续命中由Go集成测试分别验证；此处不把暖缓存耗时称为冷启动基准。

## PDF 与 CLI

exam-zh 中文 PDF 搜索“选择题”返回 3 个命中。YanTrace `doc/yantrace.pdf` 第 13 个物理页 read 返回 54 个文本块。相同 PDF 按 `region={left:10,top:5,right:95,bottom:95}`、100 DPI 渲染为 703×1053 PNG，已实际检查图像内容；区域上边界裁去部分页眉是指定裁切范围的结果，正文结构清楚。预览：[document.png](../build/validation/预览/.doc-library-preview-1527941677/document.png)。

schema 无根可用；status、list、find、grep、frequent、index 均通过。示例 JSONL 请求与一条非法行、后续有效行在同一流运行：6 行成功、1 行结构化错误，错误后继续处理并以退出码 1 结束。CLI 绝对路径/config 从不同 cwd 的调用也通过。主线另外执行的全仓 `go test`、`go vet`、`go test -race`、Linux 构建和 Windows amd64 交叉编译通过；Windows 未实际运行。

## 限制

PDF 页码是物理页，不是书内印刷页；图像裁切不是 OCR。扫描 PDF 无文字层时不能声称读到文字。PDF 文本抽取依赖 `pdftotext`，显式图片渲染另外依赖 `pdftoppm`；普通文本查询/读取不需要这些PDF工具。完整逐项命令、耗时、cache 计数和 warnings 见本机 JSON 报告。
