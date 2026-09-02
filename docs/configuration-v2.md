# v2 配置说明

v2 使用一个主配置、一个 Selection 文件和零个或多个 Override 文件。所有 YAML 文件必须声明 `configVersion: v2`，未知字段、重复字段和旧版本会直接报错。

## 主配置

默认路径为项目根目录下的 `api-doc/config.yaml`。下面是完整的最小结构：

```yaml
configVersion: v2

project:
  name: Catalog API
  version: 1.0.0
  module: example.com/catalog
  framework: gin
  openapiVersion: 3.0.3

source:
  routePackages:
    - ./routes/...
  analysisPackages:
    - ./handlers/...
    - ./model/...
    - ./respond/...

files:
  selection: selection.yaml
  overrides: overrides
  output: openapi.json
  report: report.json

responseWrappers:
  envelope:
    dataProperty: payload
    schema:
      type: object
      required: [status, payload]
      properties:
        status:
          type: string
          enum: [ok]
        payload: {}

responseFunctions:
  - symbol: example.com/catalog/respond.OK
    kind: json
    status: 200
    dataArgument: 1
    wrapper: envelope
    dataTarget: true

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

### `project`

| 字段 | 约束 |
| --- | --- |
| `name` | OpenAPI 文档标题，非空 |
| `version` | 业务 API 版本，非空 |
| `module` | 必须与项目根目录 `go.mod` 的 Module 完全一致 |
| `framework` | 固定为 `gin` |
| `openapiVersion` | 固定为 `3.0.3` |

### `source`

- `routePackages`：用于发现 Gin 路由，至少配置一个项目内相对包模式。
- `analysisPackages`：补充 Handler、Model、响应函数等分析范围，不要求固定目录分层，可以为空数组。

两组值使用 `go/packages` 包模式，例如 `./routes/...` 或 `./...`。不接受绝对路径和逃逸项目根目录的路径。

### `files`

`selection`、`overrides`、`output` 和 `report` 都相对于主配置文件所在目录解析。它们必须是项目内相对路径；绝对路径及 `..` 逃逸路径会被拒绝。

### `responseWrappers`

每个 Wrapper 显式提供：

- `schema`：OpenAPI Schema，描述响应固定字段。
- `dataProperty`：接收静态分析数据 Schema 的属性名，该属性必须存在于 `schema.properties`。

生成器不会假定固定的状态、消息或数据字段名。

### `responseFunctions`

| 字段 | 说明 |
| --- | --- |
| `symbol` | 完整 Go 符号，格式为 `module/package.Function` |
| `kind` | 仅支持 `json` 或 `binary` |
| `status` | 固定 HTTP 状态码 |
| `statusArgument` | 从调用实参读取状态码的零基下标 |
| `dataArgument` | JSON 数据实参的零基下标 |
| `wrapper` | `responseWrappers` 中定义的名称；JSON 直接响应时可省略 |
| `dataTarget` | 将该 JSON 响应标记为 Response Override 的 `data` 目标 |
| `mediaType` | `binary` 响应的媒体类型 |

`status` 和 `statusArgument` 必须且只能配置一个。`binary` 必须配置 `mediaType`，且不能配置 JSON 专用字段。`dataTarget: true` 必须同时配置 `dataArgument`。

### `parameterHelpers`

用于描述项目自定义参数读取函数。每项包含完整 `symbol`、参数位置 `location`、名称参数下标 `nameArgument` 和 OpenAPI `type`；可选字段包括 `format`、`collectionFormat` 和 `minimum`。

### `schemaMappings`

将完整 Go 类型名映射到显式 OpenAPI Schema：

```yaml
schemaMappings:
  time.Time:
    schema:
      type: string
      format: date-time
    diagnostic: false
```

`diagnostic` 决定使用该映射时是否保留诊断提示。

### `quality`

两个覆盖率字段取值为 `0` 到 `100`。其余布尔字段分别控制重复 Operation ID、OpenAPI 校验、未解析路径、未知响应、孤立 Selection 和孤立 Override 的失败门禁。

## Selection

Selection 决定每条精确 Method/Path 是否生成文档：

```yaml
configVersion: v2
defaultMode: manual
operations:
  "GET /items/{id}":
    mode: managed
    reason: public API
```

`defaultMode` 和单项 `mode` 仅支持 `managed`、`manual`、`ignored`。配置中的 Gin 参数必须使用 OpenAPI 花括号格式。

## Overrides

`files.overrides` 指向一个目录，其中 `.yaml`、`.yml` 文件按文件名顺序加载。文件结构保持如下：

```yaml
configVersion: v2
operations:
  "GET /items/{id}":
    operationId: get_item
    summary: Get item
    tags: [Items]
    response:
      status: 200
      data:
        description: Returned item
schemas:
  ItemState:
    type: string
    enum:
      - value: active
        name: Active
```

同一个 Operation 或 Schema 不能跨文件重复定义。Override 必须引用已发现的操作、参数、响应和 Schema；无效引用由 `check-overrides` 或 `validate` 报告。

## 路径和版本错误

以下配置会直接失败：

- 任意配置文件使用 `configVersion: v1` 或缺少版本。
- 主配置位于项目根目录之外。
- `files.*` 使用绝对路径或逃逸项目根目录。
- `project.module` 与 `go.mod` 不一致。
- Wrapper 的 `dataProperty` 不存在，或响应函数引用未知 Wrapper。
- 同时配置 `status` 和 `statusArgument`。
