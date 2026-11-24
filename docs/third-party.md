# 第三方组件

项目自有及从compare-figure抽取的源码采用根MIT许可证，Copyright (c) 2026 LingNc；抽取基线与文件对应见 [provenance.md](provenance.md)。以下依赖保留各自许可证，不将整份程序笼统称为“所有内容均MIT”。

| 组件 | 固定版本/来源 | 用途 | 许可与说明 |
| --- | --- | --- | --- |
| golang.org/x/image | v0.39.0 | Go内PDF预览裁切/缩放 | BSD-3-Clause，[完整LICENSE](../licenses/x-image-LICENSE) |
| gopkg.in/yaml.v3 | v3.0.1 | CLI配置解析 | 文件分别采用MIT或Apache-2.0，[完整LICENSE](../licenses/yaml-v3-LICENSE)、[NOTICE](../licenses/yaml-v3-NOTICE) |
| Poppler pdftotext/pdftoppm | 使用宿主安装版本 | PDF文字/bbox提取、按需渲染 | 独立系统进程，不随此仓库捆绑；按宿主发行版的GPL及组件许可管理 |
| Go标准库与工具链 | Go1.25+ | 原生运行、文件锁及构建 | 遵循Go发行版随附许可 |

资料源中的YanTrace、exam-zh、LuaDraw文档与示例仍由各自项目拥有和授权。本仓库只通过用户配置的目录读取它们，不复制或分发固定手册/vendor作为运行资源。运行时不加载Lua/TeX、不编译外部文档、不引入模型或OCR依赖。
