# 业务项目接入手册

本手册适用于 Gin、Go 1.25 和 OpenAPI 3.0.3。业务项目只调用 `apidoc-gen` CLI，不引入 `doc-gen` Go 依赖。

## 1. 确定项目根目录

项目根目录是包含业务 `go.mod` 的目录，不一定是 Git 仓库根目录。

- 单体仓库：通常是 Git 根目录
- 单仓多应用：例如 Git 根目录下的 `server/`

以下本地命令都从项目根目录执行：

```bash
cd <PROJECT_ROOT>
go list -m
```

记下 `go list -m` 输出，后面填写到 `project.module`。

## 2. 安装固定版本

```bash
go install github.com/topiaco/doc-gen/cmd/apidoc-gen@v0.1.0
apidoc-gen version
```

期望输出：

```text
apidoc-gen v0.1.0
```

如果找不到命令，将 `$(go env GOPATH)/bin` 加入 `PATH`。

## 3. 创建配置文件

在项目根目录创建：

```text
api-doc/
├── config.yaml
├── selection.yaml
└── overrides/
    └── base.yaml
```

`api-doc/config.yaml`：

```yaml
configVersion: v2

project:
  name: Example API
  version: 1.0.0
  module: example.com/app
  framework: gin
  openapiVersion: 3.0.3

source:
  routePackages:
    - ./router/...
  analysisPackages:
    - ./...

files:
  selection: selection.yaml
  overrides: overrides
  output: openapi.json
  report: report.json

responseWrappers: {}
responseFunctions: []
parameterHelpers: []
schemaMappings: {}

quality:
  minimumRouteDiscoveryCoverage: 100
  minimumManagedExactSchemaCoverage: 100
  failOnDuplicateOperationID: true
  failOnInvalidOpenAPI: true
  failOnUnresolvedManagedPath: true
  failOnUnknownManagedResponse: true
  failOnOrphanSelection: true
  failOnOrphanOverride: true
```

必须修改：

- `project.name` 和 `project.version`
- `project.module`：必须等于 `go list -m` 输出
- `source.routePackages`：改成实际注册 Gin 路由的包
- `source.analysisPackages`：覆盖 Handler、Model 和项目辅助函数

`api-doc/selection.yaml`：

```yaml
configVersion: v2
defaultMode: manual
operations: {}
```

`api-doc/overrides/base.yaml`：

```yaml
configVersion: v2
operations: {}
schemas: {}
```

注意：`files.*` 相对于 `api-doc/config.yaml` 所在目录解析，只能使用项目内相对路径。

## 4. 配置项目响应规则

直接调用 `gin.Context.JSON` 的项目可以先保持空配置。使用统一响应函数时，必须在主配置中声明 Wrapper 和函数完整符号：

```yaml
responseWrappers:
  success:
    dataProperty: data
    schema:
      type: object
      required: [code, message, data]
      properties:
        code:
          type: integer
        message:
          type: string
        data: {}

responseFunctions:
  - symbol: example.com/app/respond.OK
    kind: json
    status: 200
    dataArgument: 1
    wrapper: success
    dataTarget: true
```

必须按函数真实签名填写参数下标，下标从 `0` 开始。自定义参数函数、二进制响应和第三方类型映射见 [v2 配置说明](configuration-v2.md)。

## 5. 选择需要生成的接口

先列出发现的路由：

```bash
apidoc-gen list
```

在 `selection.yaml` 中把需要进入 OpenAPI 的接口标记为 `managed`：

```yaml
operations:
  "GET /items/{id}":
    mode: managed
    reason: 对外接口
```

- 路由键必须是 `大写 Method + 空格 + 完整 Path`
- Gin 的 `:id` 在配置中写成 `{id}`
- `manual` 不进入 OpenAPI
- `ignored` 用于明确不需要文档的接口
- 首次接入保持 `defaultMode: manual`，逐条增加 `managed`

## 6. 处理无法静态推断的内容

先检查问题：

```bash
apidoc-gen list-unresolved
apidoc-gen explain "GET /items/{id}"
```

只有源码无法准确表达的信息才写入 `overrides/base.yaml`，例如 `operationId`、Tags、字段约束或动态响应 Schema：

```yaml
configVersion: v2
operations:
  "GET /items/{id}":
    operationId: get_item
    tags: [Items]
    response:
      status: 200
      data:
        $ref: "#/components/schemas/Item"
schemas:
  Item:
    type: object
    required: [id]
    properties:
      id:
        type: integer
```

不要在 Override 中重复能够稳定从源码推断的内容。

## 7. 生成和验收

```bash
apidoc-gen check-overrides
apidoc-gen generate
apidoc-gen validate
go build ./...
git diff -- api-doc/openapi.json api-doc/report.json
```

验收要求：

- 所有命令退出码为 `0`
- `validate` 中所有质量门禁为 `pass`
- `openapi.json` 和 `report.json` 符合预期
- 再执行一次 `apidoc-gen generate` 后没有新 Diff
- 将配置、Overrides、`openapi.json` 和 `report.json` 一起提交

如需固定业务路由数量或关键 Schema，可增加 `api-doc/verify-generated.sh`，在 `validate` 后执行。

## 8. 配置 GitHub Actions

Workflow 必须放在 Git 仓库根目录 `.github/workflows/api-doc.yml`。下面的 `PROJECT_DIR` 填写项目根目录相对 Git 根目录的路径；项目就在 Git 根目录时填写 `.`。

```yaml
name: API Documentation

on:
  pull_request:
  push:
  workflow_dispatch:

permissions:
  contents: read

env:
  PROJECT_DIR: .

jobs:
  verify:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7

      - uses: actions/setup-go@v7
        with:
          go-version-file: ${{ env.PROJECT_DIR }}/go.mod
          cache-dependency-path: ${{ env.PROJECT_DIR }}/go.sum

      - name: Install generator
        run: |
          go install github.com/topiaco/doc-gen/cmd/apidoc-gen@v0.1.0
          APIDOC_GEN="$(go env GOPATH)/bin/apidoc-gen"
          test "$("$APIDOC_GEN" version)" = "apidoc-gen v0.1.0"
          echo "$(go env GOPATH)/bin" >> "$GITHUB_PATH"

      - name: Build project
        working-directory: ${{ env.PROJECT_DIR }}
        run: go build ./...

      - name: Generate documentation
        working-directory: ${{ env.PROJECT_DIR }}
        run: apidoc-gen generate

      - name: Validate documentation
        working-directory: ${{ env.PROJECT_DIR }}
        run: |
          apidoc-gen validate
          apidoc-gen check-overrides

      - name: Verify committed artifacts
        run: |
          git diff --exit-code -- \
            "$PROJECT_DIR/api-doc/openapi.json" \
            "$PROJECT_DIR/api-doc/report.json"

      - name: Upload artifacts
        if: always()
        uses: actions/upload-artifact@v7
        with:
          name: api-documentation
          path: |
            ${{ env.PROJECT_DIR }}/api-doc/openapi.json
            ${{ env.PROJECT_DIR }}/api-doc/report.json
          if-no-files-found: error
```

单仓多应用可增加 `paths` 过滤，但路径必须从 Git 根目录开始。CI 只重新生成并检查，不会自动提交文件。

## 9. 日常工作流程

```text
修改接口或模型
→ 调整 selection.yaml
→ 必要时调整 overrides/base.yaml
→ generate + validate
→ 检查并提交生成产物
→ PR 中由 CI 重复生成并检查 Diff
```

## 10. 接入 Apifox（可选）

`doc-gen` 只生成 OpenAPI，不直接写入 Apifox。应在 CI 校验通过后导入 `api-doc/openapi.json`。

- 首次接入先手工导入并核对接口
- 仍有人工维护接口时，使用智能合并并选择“不删除”旧资源
- 自动导入只在受信任分支的 `push` 后执行，不在 PR 中使用访问令牌
- Apifox Token 必须保存在 CI Secret 中，不能写入仓库

## 接入完成检查

- [ ] `apidoc-gen version` 为固定版本
- [ ] `project.module` 与 `go list -m` 一致
- [ ] 项目根目录和 Git 根目录已正确区分
- [ ] 三份 v2 配置存在且路径均在项目内
- [ ] 统一响应函数和 Wrapper 已按真实签名配置
- [ ] Managed 路由使用规范化 Method/Path
- [ ] `generate`、`validate`、`check-overrides` 和项目构建通过
- [ ] 连续生成无 Diff
- [ ] Workflow 位于 Git 根目录 `.github/workflows/`
- [ ] 配置和生成产物已提交
