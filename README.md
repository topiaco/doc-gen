# doc-gen

从 Go / Gin 业务源码生成 OpenAPI 3.0.3 和可离线查看的 API 文档。开发者维护业务代码和必要的注释，生成器负责分析路由、请求、响应和字段说明，无需另外维护 Schema 或接口清单。

## 1. 现有项目首次使用

使用 Go 1.26 或更新版本安装：

```bash
GOTOOLCHAIN=auto go install github.com/topiaco/doc-gen/cmd/apidoc-gen@v0.2.2
apidoc-gen version
```

`v0.2.2` 修复响应 Schema 已有具体类型，但 Example Value 的 `data` 仍显示为 `null` 的问题：联合类型和同状态码响应优先展示非空数据结构，同时保留合法的 `null` 与错误响应分支。版本命令应输出 `apidoc-gen v0.2.2`。已初始化的项目升级后直接执行 `apidoc-gen` 即可。

默认安装位置是 `go env GOPATH` 输出目录下的 `bin`，如果设置了 `GOBIN`，则使用该目录。将安装目录加入 PATH 后，即可使用 `apidoc-gen` 命令。业务项目不需要引入 doc-gen 的 Go 依赖。版本内容见 [v0.2.2 发布说明](https://github.com/topiaco/doc-gen/releases/tag/v0.2.2)。

然后在业务项目的 `go.mod` 所在目录执行：

```bash
apidoc-gen init --existing
```

这会记录初始化时已有的接口，**默认只为之后新增的接口生成文档**。因此，刚初始化且尚未新增接口时，生成的文档没有接口是正常情况。

如果希望已有接口也全部生成文档，首次初始化时使用以下命令，不加 `--existing`：

```bash
apidoc-gen init
```

两种初始化方式按需要选择一种。初始化会创建 `.apidoc/state.json`，并在 `.gitignore` 中排除生成目录。将这两个文件提交到仓库即可；`state.json` 保存已有接口记录和文档信息，不包含 Schema，无需日常维护。重复初始化不会重置记录，也不会切换上述选择。

如果仓库包含多个 Go Module，可以在命令后添加 `--project-root ./server`，其中 `server` 是业务 Module 的目录。

## 2. 本地生成

修改业务代码或注释后，在业务项目中执行：

```bash
apidoc-gen
```

生成过程已经包含必要检查，无需先执行其他命令。结果位于业务 Module 的 `.apidoc/output/`：

| 文件 | 用途 |
| --- | --- |
| `index.html` | 在浏览器中离线查看接口、字段和分析提示 |
| `openapi.json` | 导入支持 OpenAPI 3.0 的工具 |
| `report.json` | 查看生成结果及问题对应的源码位置 |

这些文件无需提交。新增接口会自动生成文档；已进入文档的接口发生修改或删除时，再次生成即可更新结果。分析过程不需要启动业务服务。

普通 Go 注释就可以提供业务说明，例如下面的代码片段省略了导入和路由初始化：

```go
// Order 订单详情
type Order struct {
    ID int64 `json:"id"` // 订单唯一标识

    // AmountCents 订单实付金额，单位为分
    AmountCents int64 `json:"amountCents"`
}

// GetOrder 查询订单详情
// 返回订单标识和实付金额
func GetOrder(c *gin.Context) {
    c.JSON(http.StatusOK, Order{ID: 1, AmountCents: 12900})
}

// Register 注册订单路由
func Register(router *gin.Engine) {
    router.GET("/orders/:id", GetOrder)
}
```

文档会使用“查询订单详情”作为标题，并展示 `Order` 的字段及业务说明。字段类型和 JSON 名称由代码确定，不需要在注释中重复填写。请求绑定、统一响应函数和特殊类型的示例见[源码注释约定](docs/source-comments.md)。

如果生成器只能确定部分结构，它仍会生成文档，并标明无法确定的位置。缺少业务描述会单独提示，因此可以根据报告修改对应源码。只有无法加载源码、注释指令无效或输出失败等实际错误才会使命令失败。

## 3. GitHub PR 自动生成

安装 `v0.2.2` 后，在业务项目中执行：

```bash
apidoc-gen init --existing --github
```

工作流会自动固定使用 `v0.2.2`，无需填写提交 SHA。也可以在首次初始化时使用这条命令，同时创建已有接口记录和工作流。项目已经初始化时，已有记录保持不变。

已有工作流将 `APIDOC_VERSION` 改为 `v0.2.2` 即可使用新版本，原有的 Go 模块安装或 Git 标签源码安装方式均可继续使用。完整工作流和源码构建方式见[使用文档](docs/integration-guide.md#3-github-pr-自动生成)。

将生成的 `.github/workflows/api-doc.yml` 与初始化文件一起提交后：

- 创建 PR 或向 PR 提交新代码时，GitHub Actions 生成预览文档
- PR 合并到默认分支后，GitHub Actions 根据合并后的代码重新生成文档
- 在仓库的 Actions 页面选择对应的 `API Documentation` 执行记录，下载 `api-documentation-<提交 SHA>`，解压后在浏览器中查看 `index.html`

Actions 摘要会列出生成结果和分析提示。工作流使用固定版本的生成器，通过同一个 `apidoc-gen` 命令生成文档。当前结果以下载文件提供，不会自动发布为在线站点。

## 更多说明

- [完整使用文档](docs/integration-guide.md)：初始化选择、已有接口、分析提示及 GitHub 设置
- [源码注释约定](docs/source-comments.md)：模型、字段、请求和响应的注释示例
- [可运行示例](examples/source/main.go)：方法 Handler、跨函数路由分组、`any` 响应函数和泛型

在 doc-gen 源码目录，使用已经安装的生成器生成示例文档：

```bash
apidoc-gen init --project-root ./examples/source
apidoc-gen --project-root ./examples/source
```

然后在浏览器中查看 `examples/source/.apidoc/output/index.html`。示例使用 `init`，因此会为全部接口生成文档。

当前版本已移除旧配置、Selection、Overrides 和 `--config` 参数。从旧版切换时，请参考[旧版项目切换说明](docs/integration-guide.md#旧版项目切换)。

## 开发验证

```bash
go test ./...
go vet ./...
go build -o /tmp/apidoc-gen ./cmd/apidoc-gen
```

验证本地源码修改时，使用 `/tmp/apidoc-gen` 执行命令。源码构建可能显示 Git 推导的版本、带 `+dirty` 的版本或 `dev`；创建 GitHub 工作流时，可以通过 `--generator-version v0.2.2` 指定正式版本。
