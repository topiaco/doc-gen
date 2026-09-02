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

业务仓库只需保留以下内容：

```text
api-doc/
├── config.yaml
├── selection.yaml
└── overrides/
    └── base.yaml
```

默认命令从项目根目录读取 `api-doc/config.yaml`：

```bash
apidoc-gen generate
apidoc-gen validate
apidoc-gen list --mode managed
apidoc-gen explain "GET /items/{id}"
apidoc-gen list-unresolved
apidoc-gen check-overrides
```

若配置文件位于其他项目内目录，可显式指定：

```bash
apidoc-gen generate --project-root /path/to/project --config docs/api/config.yaml
```

`files.*` 始终相对于主配置文件所在目录解析，且必须保持在项目根目录内部。源码包模式同样必须使用项目内相对路径。

## 配置资料

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
