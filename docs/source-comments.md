# 源码注释约定

普通 Go 注释是业务说明的主要来源。字段名、类型、JSON 名称和校验约束已经由代码表达，因此不需要另外编写 Schema。生成器无法从代码确定结构时，才使用 `apidoc:` 指令明确类型。

首次初始化、本地生成和 GitHub PR 自动生成的步骤见[项目使用说明](integration-guide.md)。本页说明哪些注释进入文档，以及如何编写这些注释。

## 描述从哪里读取

| 位置 | 文档用途 |
| --- | --- |
| Handler 声明前 | 第一行作为接口标题，剩余部分作为详细说明 |
| 类型声明前 | 模型说明，引用该类型的接口共同使用 |
| 字段声明前 | 字段说明，支持多行 |
| 字段行尾 | 简短字段说明 |
| `description` 标签 | 已有显式字段说明 |
| GORM `comment` 标签 | 没有更直接的字段说明时，使用该标签的内容 |
| 路由注册行尾 | 如果 Handler 没有标题，则作为接口标题，也适用于匿名 Handler |

同一字段有多份说明时，按以下顺序选择第一份有效说明：

```text
description 标签 > 字段前注释 > 字段行尾注释 > GORM comment
```

展示时会去掉注释开头与声明相同的 Go 名称。例如，`GetOrder 查询订单详情` 展示为“查询订单详情”，`AmountCents 订单实付金额` 展示为“订单实付金额”。

生成器只读取与相关声明和路由直接关联的注释。函数内部备注、文件版权说明、未关联到声明的注释及 `apidoc:` 指令不作为业务描述。

字段前注释如果只有 Go 字段名，去除名称后没有业务文字，那么生成器会继续使用行尾说明。多行块注释中用于排版的行首 `*` 不会出现在文档里。类型别名可以声明自己的业务说明，这份说明不会改变原类型在其他接口中的描述。

## 业务说明写什么

注释应说明业务含义。根据字段用途，写明金额和时长的单位、状态值的含义、时间约定、空值或字段省略的含义，以及字段仅在特定条件下有效的情况。不要求固定字数或统一模板，以下代码示例的注释结尾均不加句号。

缺少说明时，生成器会提示对应字段，但是不会根据变量名推测业务含义。自然语言中的状态说明只用于展示，因为它不能证明代码执行了相应校验。同样，数据库的 `not null` 和默认值只描述数据库行为，因此不会自动转换为 HTTP 接口的必填规则或默认值。

## 响应模型和 Handler 示例

以下片段省略导入和订单查询实现，`findOrder` 的返回类型是 `Order`：

```go
// Order 订单详情
// 用于展示订单金额和当前支付状态
type Order struct {
    // ID 订单唯一标识
    ID int64 `json:"id" gorm:"primaryKey"`

    // AmountCents 订单实付金额
    // 单位：分，已扣除优惠金额
    AmountCents int64 `json:"amountCents"`

    Status string `json:"status" gorm:"comment:订单状态：pending=待支付，paid=已支付"`
    Remark string `json:"remark,omitempty"` // 订单备注；为空时不返回
    InternalNote string `json:"-"` // 内部处理备注
}

// Response 接口响应
type Response[T any] struct {
    Data T `json:"data"` // 返回数据
}

// GetOrder 查询订单详情
// 返回订单金额和当前支付状态
func GetOrder(c *gin.Context) {
    order := findOrder(c.Param("id"))
    c.JSON(http.StatusOK, Response[Order]{Data: order})
}

// Register 注册订单路由
func Register(router *gin.Engine) {
    router.GET("/orders/:id", GetOrder)
}
```

生成结果如下：

| 文档内容 | 来源和结果 |
| --- | --- |
| 接口 | `GET /orders/{id}`，路径参数为 `id` |
| 标题 | Handler 第一行注释：“查询订单详情” |
| 详细说明 | Handler 后续注释：“返回订单金额和当前支付状态” |
| HTTP 200 响应 | `Response[Order]`，其中 `data` 使用 `Order` 的字段和说明 |
| `data.amountCents` | 整数，包含金额含义和“单位：分”的说明 |
| `data.status` | 字符串，说明来自 GORM 的 `comment` |
| `data.remark` | 字符串，包含行尾说明；`omitempty` 表示允许省略 |
| `InternalNote` | 带有 `json:"-"`，不出现在 JSON 文档中 |

如果接口直接返回数据库模型，就使用该模型的结构和说明。如果接口返回独立的请求或响应类型，就以实际使用的类型为准，不会自动添加其他数据库字段。同一模型的注释修改一次，重新生成后，所有引用它的接口说明都会更新。

### 统一响应函数中的 any

项目已有统一响应函数时，生成器会继续分析调用处的具体值。以下代码可以使用前面的 `Order` 和 `Response`，不需要配置函数参数位置或响应 Schema：

```go
// Respond 返回包含 data 的 JSON 响应
func Respond(c *gin.Context, data any) {
    c.JSON(http.StatusOK, Response[any]{Data: data})
}

// GetOrder 查询订单详情
func GetOrder(c *gin.Context) {
    order := findOrder(c.Param("id"))
    Respond(c, order)
}
```

因为 `order` 的类型是 `Order`，而函数内部的响应结构也能确定，所以生成器会为 `data` 生成完整的订单结构。参数声明为 `any` 不代表必须添加显式类型注释。

完整可运行代码见 [examples/source](../examples/source/main.go)，其中还包含方法 Handler、跨函数路由分组和匿名 Handler。

## 请求模型和绑定示例

请求类型必须实际用于 Handler 的绑定调用。下面省略导入和保存订单的业务实现：

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

生成器会为 `POST /orders` 生成 JSON 请求体，将客户 ID 标为必填，并将备注长度上限设为 200。但是，`json:",omitempty"` 表示序列化时可以省略字段，不是请求必填规则；请求约束以实际绑定和校验为准。

生成器按 Gin 默认的 `binding` 标签读取校验规则，不会仅凭 `validate` 标签推断请求约束。GET 请求中的 `ShouldBind` 按查询参数分析；明确使用 JSON、查询参数、URI 或 Header 绑定时，则读取对应的字段标签。

集合标签中的 `dive` 表示之后的规则用于元素，因此元素的 `required` 不会使集合本身成为必填。代码为缺失的查询参数设置默认值时，该参数仍然可选；只有能够确认代码拒绝缺失参数时，才根据这段校验逻辑标记必填。

## 无法推断时使用显式注释

| 注释 | 源码位置 | 含义 |
| --- | --- | --- |
| `apidoc:request CreateOrderRequest` | Handler 声明前 | 明确 JSON 请求体类型 |
| `apidoc:response 200 Response[Order]` | Handler 声明前 | 明确 HTTP 200 的完整 JSON 响应类型 |
| `apidoc:type string` | 类型或字段声明处 | 明确实际序列化后的类型 |
| `apidoc:format date` | 类型或字段声明处 | 明确实际序列化后的格式，此例为日期 |
| `apidoc:include` / `apidoc:ignore` | 路由注册旁 | 为该接口生成文档，或排除该接口 |

请求和响应注释引用所在 Go 文件可见的类型，支持导入别名、指针、切片及泛型。指令只修改明确指定的请求体或响应状态码，因此其他已识别的响应仍然保留。引用的类型必须真实存在；如果类型名称或指令有误，命令会失败并指出源码位置。

例如，响应由运行时信息决定，无法根据代码确定时，可以直接引用已有类型：

```go
// GetOrder 查询订单详情
// apidoc:response 200 Response[Order]
func GetOrder(c *gin.Context) {
    // 此处省略由运行时信息决定响应类型的业务实现
}
```

`apidoc:response` 指定的是包含所有外层字段的完整响应类型。例如，接口实际返回 `{"data": ...}` 时，应引用 `Response[Order]`，而不是仅引用 `Order`。

如果自定义序列化的结果无法从代码确定，可以在已有类型声明处说明一次，所有引用该类型的接口都会使用这份说明：

```go
// BusinessDate 业务日期，格式为 YYYY-MM-DD
// apidoc:type string
// apidoc:format date
type BusinessDate struct {
    // 此处省略已有字段及自定义序列化实现
}
```

`apidoc:type` 支持 `string`、`integer`、`number`、`boolean` 和 `object`。`apidoc:format` 用于说明该类型的格式，例如 `date` 或 `date-time`。

这些注释只影响生成的文档，不改变业务代码的序列化行为，因此必须与类型的实际实现一致。如果无法修改第三方类型，可以在使用它的字段或本地已有类型别名处添加指令。如果生成器已经能够确定该类型的结构，则无需添加这些指令。

路由的 `apidoc:include` 和 `apidoc:ignore` 用法见[本地生成说明](integration-guide.md#已有接口和新增接口如何处理)。
