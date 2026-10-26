# 项目使用说明

日常只需维护业务代码和必要的业务注释，然后执行 `apidoc-gen`。以下按照首次使用、本地生成和 GitHub PR 自动生成的顺序说明。

## 1. 现有项目首次使用

### 准备生成器

使用 Go 1.26 或更新版本安装：

```bash
GOTOOLCHAIN=auto go install github.com/topiaco/doc-gen/cmd/apidoc-gen@v0.2.2
apidoc-gen version
```

`v0.2.2` 修复响应 Schema 已有具体类型，但 Example Value 的 `data` 仍显示为 `null` 的问题：联合类型和同状态码响应优先展示非空数据结构，同时保留合法的 `null` 与错误响应分支。版本命令应输出 `apidoc-gen v0.2.2`。已初始化的项目升级后直接执行 `apidoc-gen` 即可，已有接口记录保持不变。

默认安装位置是 `go env GOPATH` 输出目录下的 `bin`，如果设置了 `GOBIN`，则使用该目录。将安装目录加入 PATH 后，即可使用 `apidoc-gen` 命令。版本内容见 [v0.2.2 发布说明](https://github.com/topiaco/doc-gen/releases/tag/v0.2.2)。

业务项目不需要引入 doc-gen 的 Go 依赖。但是，生成器需要读取业务项目的类型信息，因此业务代码及其依赖必须能够被 Go 工具加载。分析过程使用只读模块模式，不会修改业务项目的 `go.mod` 或 `go.sum`，也不执行应用、`init` 函数或数据库查询。

### 选择已有接口是否生成文档

在业务项目的 `go.mod` 所在目录，根据需要选择一种初始化方式：

| 希望生成哪些接口 | 首次初始化命令 |
| --- | --- |
| 只生成今后新增的接口 | `apidoc-gen init --existing` |
| 已有接口和今后新增的接口都生成 | `apidoc-gen init` |

`--existing` 会记录初始化时发现的已有接口，并在生成时排除它们。因此，初始化之后尚未新增接口时，生成结果中没有接口是正常情况。如果只有部分已有接口需要生成文档，可以在这些路由旁添加 `apidoc:include`，示例见下文。

初始化会创建或修改以下文件：

| 文件 | 内容 | 是否提交 |
| --- | --- | --- |
| `.apidoc/state.json` | Module 名、文档标题、API 版本和已有接口记录 | 是 |
| `.gitignore` | 增加 `/.apidoc/output/`，排除生成文件 | 是 |

`state.json` 以“HTTP 方法＋规范化路径”记录已有接口，不包含字段或 Schema，因此无需随模型变化而人工修改。**重复初始化会保留已有记录和文档信息，不会切换生成范围。** 例如，已经执行过 `init --existing`，再执行 `init` 不会让已有接口自动进入文档。

首次初始化时，可以使用 `--title` 和 `--api-version` 设置文档标题和 API 版本；默认值分别为 Module 名和 `unversioned`：

```bash
apidoc-gen init --existing --title '订单 API' --api-version '1.0'
```

### 确定业务目录

在 Module 子目录执行时，工具会向上查找 `go.mod`。从不含 `go.mod` 的仓库根目录执行时，如果仓库中只有一个 Module，工具会自动选择它；如果有多个 Module，则明确指定业务目录：

```bash
apidoc-gen init --existing --project-root ./server
```

这里的 `server` 是示例目录名，实际使用时替换为业务 Module 的路径。后续生成也可以使用同一个 `--project-root` 参数。

如果初始化时无法确定某条路由的方法或路径，工具会提示源码位置，并记录其余已识别的接口。以后能够识别这条路由时，它会被视为新增接口，因为首次记录中没有它。

## 2. 本地生成

### 生成和查看文档

在业务项目中执行：

```bash
apidoc-gen
```

如果在仓库根目录为子目录中的 Module 生成文档，则执行：

```bash
apidoc-gen --project-root ./server
```

生成器一次完成源码分析、业务注释提取、必要检查和文件输出。结果位于所选 Module 的 `.apidoc/output/`：

| 文件 | 使用方式 |
| --- | --- |
| `index.html` | 在浏览器中离线查看，支持搜索接口和字段、查看嵌套结构及问题对应的源码位置 |
| `openapi.json` | 导入支持 OpenAPI 3.0 的工具 |
| `report.json` | 查看接口列表、生成数量和分析问题 |

这些文件无需提交。每次修改接口代码或模型注释后，再次执行 `apidoc-gen` 即可更新它们，不需要启动业务服务。

`apidoc-gen generate` 与 `apidoc-gen` 相同。生成过程已经包含检查，因此日常不需要先执行 `validate`；如果只想查看分析结果而不写入文件，可以使用 `apidoc-gen validate`。

### 已有接口和新增接口如何处理

接口由 HTTP 方法和规范化路径共同确定。例如，Gin 的 `GET /orders/:id` 在文档中表示为 `GET /orders/{id}`。

| 代码变化 | 再次生成时的结果 |
| --- | --- |
| 新增方法或路径 | 自动为新接口生成文档 |
| 修改已进入文档的接口参数、响应或说明 | 更新对应文档 |
| 删除路由 | 从文档中删除对应接口 |
| 只修改已被 `--existing` 排除的接口实现 | 仍然不生成该接口的文档 |
| 改变已有接口的方法或路径 | 视为新接口 |

如果希望为某个已有接口生成文档，或者排除某个接口，在对应路由注册前或行尾添加指令：

```go
// Register 注册订单和内部检查路由
func Register(router *gin.Engine) {
    // apidoc:include
    router.GET("/orders/:id", GetOrder)

    router.GET("/internal/health", Health) // apidoc:ignore
}
```

指令只作用于相邻的那条路由注册，因此无需另外维护接口清单。

### 用业务注释说明字段

优先使用普通 Go 注释。下面的请求类型通过实际的 `ShouldBindJSON` 调用用于接口，生成器因此能够确定请求字段和校验规则。示例省略导入和保存订单的业务实现：

```go
// CreateOrderRequest 创建订单的请求参数
type CreateOrderRequest struct {
    CustomerID int64 `json:"customerId" binding:"required"` // 客户 ID
    Remark string `json:"remark" binding:"max=200"` // 订单备注
}

// CreateOrder 创建订单
// 根据客户信息创建订单，可填写备注
func CreateOrder(c *gin.Context) {
    var request CreateOrderRequest
    if err := c.ShouldBindJSON(&request); err != nil {
        c.Status(http.StatusBadRequest)
        return
    }
    c.Status(http.StatusCreated)
}

// Register 注册创建订单的路由
func Register(router *gin.Engine) {
    router.POST("/orders", CreateOrder)
}
```

文档标题为“创建订单”，请求体包含必填的 `customerId` 和长度不超过 200 的 `remark`。这些约束来自 `binding` 标签，字段说明来自行尾注释；无需再维护一份请求 Schema。

同一模型的说明只需在声明处维护一次，所有引用它的接口都会使用。模型、统一响应函数、特殊类型和显式类型注释的完整约定见[源码注释说明](source-comments.md)。

### 根据分析提示修改源码

生成器会根据调用实参、变量赋值、返回类型和泛型分析请求与响应。参数声明为 `any` 时，也会继续分析传入的具体值。如果仍有无法确定的信息，生成结果会明确说明：

| 提示情况 | 生成结果及处理方式 |
| --- | --- |
| 缺少业务描述 | 保留结构，提示对应字段；在模型或字段处写明业务含义 |
| 部分类型无法确定 | 保留已知字段并标明未知部分；必要时用 `apidoc:` 引用已有 Go 类型 |
| 整个响应无法确定 | 使用注明“无法从源码确定响应结构”的 `default` 响应，不假设成功状态码 |
| 路由方法或路径无法确定 | 报告源码位置，不生成猜测的接口路径 |

同一模型的问题会合并显示，并列出受影响的接口。工具不会根据字段名称编造业务说明，也不会把数据库的 `not null` 或默认值当作接口约束。

上述提示不会使生成失败。只有源码无法加载、显式指令无效、接口冲突、生成的 OpenAPI 无效或文件写入失败等实际错误才会使命令失败。类型注释可以说明请求或响应结构，但是不能确定只在运行时才知道的路由路径。

## 3. GitHub PR 自动生成

### 创建工作流

安装 `v0.2.2` 后，在业务项目中执行：

```bash
apidoc-gen init --existing --github
```

工作流会使用当前安装版本，自动将 `APIDOC_VERSION` 写为 `v0.2.2`。因此，本地和 GitHub 使用相同版本，无需填写提交 SHA。

如果这是首次初始化，并且希望全部已有接口也生成文档，则不加 `--existing`。如果已经初始化，重复执行只会沿用已有记录，不会重置它。

工具会在 Git 根目录创建 `.github/workflows/api-doc.yml`。如果业务 Module 位于 `server` 等子目录，初始化时添加 `--project-root ./server`，工具会将该目录写入工作流。提交工作流、业务 Module 中的 `.apidoc/state.json` 和 `.gitignore` 即可。

如果该工作流文件已存在且内容不同，命令会提示，不会覆盖它。已有工作流升级时，将其中的版本改为：

```yaml
env:
  APIDOC_VERSION: "v0.2.2"
```

保留 `PROJECT_DIR` 和其他已有设置。如果原有安装步骤使用 `go install "github.com/topiaco/doc-gen/cmd/apidoc-gen@$APIDOC_VERSION"`，无需修改该步骤。当前生成器创建的工作流从 Git 标签或提交构建，安装步骤如下：

```yaml
      - name: Install documentation generator
        env:
          GOTOOLCHAIN: auto
        run: |
          generator_dir=$(mktemp -d)
          git clone --quiet https://github.com/topiaco/doc-gen.git "$generator_dir"
          git -C "$generator_dir" checkout --quiet --detach "$APIDOC_VERSION"
          cd "$generator_dir"
          go install ./cmd/apidoc-gen
```

这会从指定 Git 标签或提交构建生成器。`GOTOOLCHAIN: auto` 允许业务项目使用较早 Go 版本时自动选择生成器所需的工具链。依赖仍按 `go.sum` 正常校验。

### 从源码构建时创建工作流

如果需要验证本地修改，可以在 doc-gen 源码目录构建临时命令：

```bash
go build -o /tmp/apidoc-gen ./cmd/apidoc-gen
```

源码构建可能显示 Git 推导的版本、带 `+dirty` 的版本或 `dev`。在业务项目中创建工作流时，明确指定已发布的标签或提交：

```bash
/tmp/apidoc-gen init --existing --github --generator-version v0.2.2
```

这里指定的是 GitHub 使用的生成器版本；本地尚未发布的修改不会包含在 `v0.2.2` 中。需要验证另一个已发布提交时，也可以将版本号换成该提交的完整 40 位 SHA。

### PR 和合并后的行为

| 操作 | GitHub Actions 的行为 |
| --- | --- |
| 创建或重新开启 PR、向 PR 提交新代码 | 为本次 PR 检查生成预览文档 |
| 合并到默认分支，或直接向默认分支提交代码 | 根据默认分支的新提交生成文档 |
| 在 Actions 中手动执行工作流 | 为所选版本生成文档 |

工作流使用业务项目 `go.mod` 中的 Go 版本，安装固定版本的生成器，下载业务依赖，然后执行 `apidoc-gen` 并上传三个生成文件。本地与 CI 使用同一生成命令。

生成器与业务项目可能要求不同的 Go 版本，因此安装生成器的步骤会允许 Go 自动选择所需工具链。该设置只用于安装步骤，后续业务依赖下载和源码分析仍使用项目配置的 Go 版本。

### 查看 PR 文档

1. 在仓库的 Actions 页面选择对应的 `API Documentation` 执行记录
2. 查看 Summary 中的生成数量和分析提示
3. 在 Artifacts 中下载 `api-documentation-<提交 SHA>`
4. 解压后在浏览器中查看 `index.html`，同一目录还包含 `openapi.json` 和 `report.json`

下载文件的名称包含本次工作流的提交 SHA，可以根据执行记录区分不同版本的文档。文件保留期遵循仓库设置。

当前工作流提供可下载的文档文件，不会自动创建在线预览地址，也不会向 PR 发送评论。代码合并后会重新生成文档，因此默认分支的结果反映的是合并后的代码。

## 旧版项目切换

当前版本已移除旧配置、Selection、Overrides、`--config` 参数，以及 `list`、`explain`、`list-unresolved`、`check-overrides` 命令。

切换时，先在相关模型、字段和 Handler 的注释中保留仍然需要的业务说明。只有无法从代码识别的类型才使用显式指令，具体示例见[源码注释约定](source-comments.md)。

然后按本文的首次使用步骤初始化并生成文档。全部已有接口都需要生成时使用 `init`；只希望默认生成新增接口时使用 `init --existing`，并为个别需要生成的已有接口添加 `apidoc:include`。

比较生成的 OpenAPI 和离线页面，确认接口范围、请求、响应和说明符合预期后，将原工作流改为执行 `apidoc-gen`，并删除业务仓库中已不再使用的旧文档配置。
