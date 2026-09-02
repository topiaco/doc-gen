# doc-gen

`doc-gen` 从 Go 源码静态分析 Gin 路由、请求和响应，生成 OpenAPI 3.0.3 文档及分析报告。业务项目只需要保存配置文件并调用 `apidoc-gen` CLI，不需要引入 Go Library 依赖。

当前支持范围：

- Go 1.25
- Gin
- OpenAPI 3.0.3
- v2 配置，不兼容 v1

## 安装

首个公开版本发布后可固定安装：

```bash
go install github.com/topiaco/doc-gen/cmd/apidoc-gen@v0.1.0
apidoc-gen version
```

在发布前或参与仓库开发时，可以从干净的仓库目录构建：

```bash
go build -o /tmp/apidoc-gen ./cmd/apidoc-gen
```

## 最小示例

仓库包含一个可独立分析的 Gin 项目：[examples/minimal](examples/minimal)。从仓库根目录执行：

```bash
/tmp/apidoc-gen generate --project-root ./examples/minimal
/tmp/apidoc-gen validate --project-root ./examples/minimal
```

生成结果位于：

- `examples/minimal/api-doc/openapi.json`
- `examples/minimal/api-doc/report.json`

重复执行 `generate` 会产生逐字节一致的结果。

## 接入业务项目

新项目直接按照[业务项目接入手册](docs/integration-guide.md)完成目录、v2 配置、接口选择、生成验收和 CI 接入。单仓多应用必须区分 Git 根目录与包含 `go.mod` 的项目根目录。

## 配置资料

- [业务项目接入手册与工作流程](docs/integration-guide.md)
- [v2 配置说明](docs/configuration-v2.md)
- [v1 到 v2 迁移说明](docs/migration-v1-to-v2.md)
- [主配置 JSON Schema](internal/config/schemas/config-v2.json)
- [Selection JSON Schema](internal/config/schemas/selection-v2.json)
- [Override JSON Schema](internal/config/schemas/override-v2.json)

## 开发验证

```bash
go test ./...
go vet ./...
go build ./cmd/apidoc-gen
```
