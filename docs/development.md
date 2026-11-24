# 开发与验证

核心是一个Go模块：根包 `doclibrary`，命令入口 `cmd/doc-library`；程序调用示例在 `examples/go-client`。生产代码不依赖Python、模型SDK或绘图runner。PDF文字与渲染调用Poppler宿主程序。

```bash
GOCACHE="$PWD/build/go-cache" go test ./...
GOCACHE="$PWD/build/go-cache" go vet ./...
GOCACHE="$PWD/build/go-cache" go test -race ./...
GOCACHE="$PWD/build/go-cache" go build -o build/doc-library ./cmd/doc-library
```

本项目提取代码继承compare-figure的MIT来源说明。修改通用提取/定位行为时，保持来源记录，不把旧绘图项目的模型、编译与会话管理带入此库；对方将来如迁移调用此库，应通过公开API，避免两个独立通用实现继续分叉。

单元与集成测试只创建小型匿名文本或通过标准库构造的PDF，不把外部官方手册和用户全文提交到testdata。PDF测试实际调用Poppler；宿主缺失只跳过对应的PDF检查。跨平台构建不是在该平台实际运行验证。真实资料运行与结果见 [validation.md](validation.md)。

缓存属于可重建状态：源码Git只跟踪程序、测试和说明；文档原本仍在各自只读根，缓存和频次数据、预览与诊断日志都在忽略目录。不要把生成的提取全文或用户本机配置加入提交。
