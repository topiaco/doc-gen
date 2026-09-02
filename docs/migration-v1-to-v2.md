# v1 到 v2 迁移

v2 不包含 v1 兼容层。主配置、Selection 和所有 Override 文件必须一次性升级；任何 v1 文件都会返回明确的版本不支持错误。

## 1. 更新全部配置版本

将每个 YAML 文件的版本改为：

```yaml
configVersion: v2
```

## 2. 合并源码分析范围

删除固定分层字段：

```yaml
source:
  routePackages: [./routes/...]
  controllerPackages: [./handlers/...]
  servicePackages: [./service/...]
  modelPackages: [./model/...]
```

改为：

```yaml
source:
  routePackages: [./routes/...]
  analysisPackages:
    - ./handlers/...
    - ./service/...
    - ./model/...
```

`analysisPackages` 只表示需要加载的源码范围，不要求业务项目采用特定目录结构。

## 3. 删除 `analysis` 分组

移除整个 `analysis:` 分组。v1 中的 `conservative`、`inferManualRequiredChecks` 和 `fieldDescriptionFallback` 不再接受。

## 4. 调整文件路径

v2 的 `files.*` 相对于主配置文件目录，而不是项目根目录。

当主配置为 `api-doc/config.yaml` 时：

```yaml
files:
  selection: selection.yaml
  overrides: overrides
  output: openapi.json
  report: report.json
```

所有路径必须是项目内部相对路径。

## 5. 显式定义响应包装

将响应固定字段写入 `responseWrappers`，并通过 `dataProperty` 指定动态数据位置：

```yaml
responseWrappers:
  envelope:
    dataProperty: payload
    schema:
      type: object
      required: [status, payload]
      properties:
        status:
          type: string
        payload: {}
```

生成器不再内置任何 Wrapper 名称或响应字段名。

## 6. 更新响应函数

所有 JSON Helper 的 `kind` 统一为 `json`，二进制 Helper 使用 `binary`：

```yaml
responseFunctions:
  - symbol: example.com/catalog/respond.OK
    kind: json
    status: 200
    dataArgument: 1
    wrapper: envelope
    dataTarget: true
```

原先代表成功、HTTP 错误或业务错误的 `kind` 值不再使用；差异由各函数引用的 Wrapper Schema 表达。需要接收 Response Override `data` 的响应函数显式设置 `dataTarget: true`。

## 7. 验证迁移结果

```bash
apidoc-gen list --mode managed
apidoc-gen check-overrides
apidoc-gen validate
apidoc-gen generate
```

连续执行两次 `generate` 并比较 `openapi.json`、`report.json`，确认生成结果稳定后再切换 CI。
