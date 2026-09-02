# 业务项目接入手册

本手册适用于 Gin、Go 1.25 和 OpenAPI 3.0.3。业务项目只调用 `apidoc-gen` CLI，不引入 `doc-gen` Go 依赖。除 CI 章节外，以下命令都从包含业务 `go.mod` 的目录执行。

## 1. 确定项目根目录

这里的“项目根目录”是包含业务 `go.mod` 的 Go Module 根目录，不一定是 Git 仓库根目录。

- 单体仓库：通常是 Git 根目录
- 单仓多应用：例如 Git 根目录下有 `server/` 和 `website/`，项目根目录是 `server/`
- `api-doc/` 放在项目根目录；只使用前端技术的 `website/` 不需要配置
- `.github/workflows/` 始终放在 Git 根目录

从 Git 根目录确认 Module。单体仓库将 `PROJECT_DIR` 设为 `.`，`go.mod` 位于 `server/` 时设为 `server`：

```bash
PROJECT_DIR=server
go -C "$PROJECT_DIR" list -m
cd "$PROJECT_DIR"
go list -m
```

两次输出必须一致。记下该值，后面填写到 `project.module`。不要在不含 `go.mod` 的 Git 根目录直接执行 `go list -m`，也不需要为此新增 `go.mod` 或 `go.work`。

## 2. 安装固定版本

```bash
go install github.com/topiaco/doc-gen/cmd/apidoc-gen@v0.1.0
APIDOC_BIN_DIR="$(go env GOBIN)"
if [ -z "$APIDOC_BIN_DIR" ]; then
  APIDOC_BIN_DIR="$(go env GOPATH)/bin"
fi
export PATH="$APIDOC_BIN_DIR:$PATH"
apidoc-gen version
```

期望输出：

```text
apidoc-gen v0.1.0
```

安装只写入本机 Go Bin 目录，不修改业务项目的 `go.mod` 或 `go.sum`。

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

## 5. 选择接口纳管策略

先列出发现的路由：

```bash
apidoc-gen list
```

选择以下一种策略，不要混用。

### 新项目：全部接口自动纳管

```yaml
configVersion: v2
defaultMode: managed
operations: {}
```

未显式登记的路由都会进入 OpenAPI。只有确定不生成的接口才单独设置为 `manual` 或 `ignored`。

### 已有项目：只自动纳管后续新增接口

先保持 `defaultMode: manual` 和空 `operations`，确认 `apidoc-gen list --mode manual` 已发现全部存量路由。然后只在首次接入时生成存量 Selection：

```bash
apidoc-gen list --mode manual | awk -F '\t' '
BEGIN {
  print "configVersion: v2"
  print "defaultMode: managed"
  print "operations:"
}
$1 == "manual" {
  printf "  \"%s\":\n", $2
  print "    mode: manual"
  print "    reason: 存量接口，不纳入自动生成"
}
' > api-doc/selection.generated.yaml
```

检查 `selection.generated.yaml` 的路由数量和内容，把下载文件、健康检查等明确不生成的接口改为 `ignored`，确认无误后替换：

```bash
mv api-doc/selection.generated.yaml api-doc/selection.yaml
apidoc-gen validate
```

此后不要把新接口补成 `manual`。新接口未出现在 Selection 中，会通过 `defaultMode: managed` 自动进入 OpenAPI。删除存量路由时要删除对应 Selection；修改存量路由的 Method 或 Path 时，要同步更新其精确路由键。

### 已有项目：逐条纳管

保持 `defaultMode: manual`，只把确认进入 OpenAPI 的接口显式设置为 `managed`：

```yaml
operations:
  "GET /items/{id}":
    mode: managed
    reason: 对外接口
```

- 路由键必须是 `大写 Method + 空格 + 完整 Path`
- Gin 的 `:id` 在配置中写成 `{id}`
- `manual` 不进入 OpenAPI
- `ignored` 用于下载文件、健康检查等明确不生成的接口

## 6. 处理无法静态推断的内容

先查看实际纳管接口，再按需检查某条接口：

```bash
apidoc-gen list --mode managed
apidoc-gen explain "GET /items/{id}"
apidoc-gen list-unresolved
```

`list-unresolved` 是全量静态分析诊断，会同时列出 `managed`、`manual` 和 `ignored` 的警告，并且出现警告时仍可返回退出码 `0`。它不是验收门禁，也不要求结果为 `0`。`manual` 和 `ignored` 不生成 OpenAPI，其诊断无需处理。

`managed` 接口出现在诊断中时先用 `explain` 复核；只有 `validate` 的 `unknownResponses` 非 `0` 或质量门禁为 `fail` 时必须修正。只有源码无法准确表达的信息才写入 `overrides/base.yaml`。

Override 可补充 `operationId`、Tags、字段约束或动态响应 Schema：

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
go build ./...
go test ./...
apidoc-gen list --mode managed
apidoc-gen check-overrides
apidoc-gen validate
apidoc-gen generate
git diff -- api-doc/openapi.json api-doc/report.json
```

命令作用：

| 命令 | 用途 | 是否要求无输出 |
| --- | --- | --- |
| `list --mode managed` | 确认实际生成范围 | 否 |
| `list-unresolved` | 按需查看所有模式的分析诊断 | 否，且不作为门禁 |
| `check-overrides` | 校验 Override 引用 | 要求 `errors=0` |
| `validate` | 执行质量门禁，不写生成文件 | 要求全部 `pass` |
| `generate` | 通过门禁后写入 OpenAPI 和报告 | 要求退出码为 `0` |

验收要求：

- 构建、测试、`check-overrides`、`validate` 和 `generate` 退出码为 `0`
- `list --mode managed` 与本次预期纳管范围一致
- `validate` 中所有质量门禁为 `pass`
- `validate` 的 `unknownResponses=0`
- `openapi.json` 和 `report.json` 符合预期
- 连续执行两次 `generate`，文件内容保持一致
- 将配置、Overrides、`openapi.json` 和 `report.json` 一起提交

`list-unresolved` 中仅属于 `manual` 或 `ignored` 的警告不影响验收，不要为清空这些警告增加 Override。

如需固定业务路由数量或关键 Schema，可增加 `api-doc/verify-generated.sh`，在 `validate` 后执行。

## 8. 配置 GitHub Actions

Workflow 必须放在 Git 仓库根目录 `.github/workflows/api-doc.yml`。下面的 `PROJECT_DIR` 填写项目根目录相对 Git 根目录的路径：`go.mod` 在 Git 根目录时填写 `.`，在 `server/` 时填写 `server`。

```yaml
name: API Documentation

on:
  pull_request:
  push:
  workflow_dispatch:

permissions:
  contents: read

env:
  PROJECT_DIR: server

jobs:
  verify:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6

      - uses: actions/setup-go@v7
        with:
          go-version-file: ${{ env.PROJECT_DIR }}/go.mod
          cache-dependency-path: ${{ env.PROJECT_DIR }}/go.sum

      - name: Install generator
        run: |
          go install github.com/topiaco/doc-gen/cmd/apidoc-gen@v0.1.0
          APIDOC_BIN_DIR="$(go env GOBIN)"
          if [ -z "$APIDOC_BIN_DIR" ]; then
            APIDOC_BIN_DIR="$(go env GOPATH)/bin"
          fi
          test "$("$APIDOC_BIN_DIR/apidoc-gen" version)" = "apidoc-gen v0.1.0"
          echo "$APIDOC_BIN_DIR" >> "$GITHUB_PATH"

      - name: Build project
        working-directory: ${{ env.PROJECT_DIR }}
        run: |
          go build ./...
          go test ./...

      - name: Check documentation overrides
        working-directory: ${{ env.PROJECT_DIR }}
        run: apidoc-gen check-overrides

      - name: Validate documentation
        working-directory: ${{ env.PROJECT_DIR }}
        run: apidoc-gen validate

      - name: Generate documentation
        working-directory: ${{ env.PROJECT_DIR }}
        run: apidoc-gen generate

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

单仓多应用可增加 `paths` 过滤，但路径必须从 Git 根目录开始，例如 `server/**`。CI 只重新生成并检查，不会自动提交文件；Diff 失败表示开发者尚未提交最新生成产物。

## 9. 日常工作流程

```text
修改接口或模型
→ list --mode managed 确认纳管范围
→ validate
→ 仅在 managed 接口无法推断时调整 overrides/base.yaml
→ generate
→ 检查并提交生成产物
→ PR 中由 CI 重复生成并检查 Diff
```

使用 `defaultMode: managed` 时，新增接口不需要修改 Selection。只有明确排除接口，或删除、修改已登记的存量接口时才调整 `selection.yaml`。

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
- [ ] 已明确选择 Selection 策略，Managed 路由使用规范化 Method/Path
- [ ] “只纳管新接口”模式已把全部存量路由显式登记为 `manual` 或 `ignored`
- [ ] `generate`、`validate`、`check-overrides` 和项目构建通过
- [ ] `validate` 全部为 `pass` 且 `unknownResponses=0`
- [ ] 未把 `manual`、`ignored` 的 `list-unresolved` 警告误当成失败
- [ ] 连续生成无 Diff
- [ ] Workflow 位于 Git 根目录 `.github/workflows/`
- [ ] 配置和生成产物已提交
