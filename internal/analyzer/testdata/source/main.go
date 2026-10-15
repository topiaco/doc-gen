package source

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

// Order 订单详情
// 用于展示实付金额及支付状态
type Order struct {
	// ID 订单唯一标识
	ID int64 `json:"id" gorm:"primaryKey"`
	// AmountCents 订单实付金额
	// 单位：分，已扣除优惠金额
	AmountCents  int64  `json:"amountCents"`
	Status       string `json:"status" gorm:"comment:订单状态：pending=待支付，paid=已支付"`
	Remark       string `json:"remark,omitempty"` // 订单备注；为空时不返回
	InternalNote string `json:"-"`                // 内部备注
}

// Response 接口响应
type Response[T any] struct {
	Data T `json:"data"` /* 返回数据 */
}

// CreateOrderRequest 创建订单的请求参数
type CreateOrderRequest struct {
	CustomerID int64  `json:"customerId" binding:"required"` // 客户 ID
	Remark     string `json:"remark" binding:"max=200"`      // 订单备注
}

// Day 业务日期
// apidoc:type string
// apidoc:format date
type Day struct{ Value int }

// MarshalJSON 将测试日期编码为字符串
func (Day) MarshalJSON() ([]byte, error) { return []byte(`"2026-10-15"`), nil }

// Query 查询条件
type Query struct {
	Page int `form:"page" binding:"required,min=1" json:"-"` // 页码
}

// respond 将具体业务值作为统一响应的数据字段
func respond(c *gin.Context, value any) {
	data := value
	c.JSON(http.StatusOK, Response[any]{Data: data})
}

// bind 将 JSON 请求绑定到调用处提供的类型
func bind(c *gin.Context, out any) { c.ShouldBindJSON(out) }

// findOrder 返回用于类型推断测试的订单
func findOrder(string) Order { return Order{} }

type Handler struct{}

// Get 查询订单详情
// 返回订单金额和当前支付状态
func (Handler) Get(c *gin.Context) {
	order := findOrder(c.Param("id"))
	respond(c, order)
}

// Create 创建订单
func Create(c *gin.Context) {
	var req CreateOrderRequest
	bind(c, &req)
	respond(c, Order{ID: req.CustomerID, Remark: req.Remark})
}

// Dynamic 动态响应
// apidoc:response 202 Response[Order]
func Dynamic(*gin.Context) {}

// Missing 尚未声明返回类型
func Missing(*gin.Context) {}

// Register 创建业务路由组并调用注册函数
func Register(engine *gin.Engine) { register(engine.Group("/api")) }

// register 注册用于验证源码分析的接口
func register(group *gin.RouterGroup) {
	h := Handler{}
	group.GET("/orders/:id", h.Get)
	group.POST("/orders", Create)
	group.GET("/dynamic", Dynamic)
	group.GET("/missing", Missing)
	group.GET("/closure", func(c *gin.Context) {
		var query Query
		c.ShouldBindQuery(&query)
		limit, _ := strconv.Atoi(c.Query("limit"))
		c.JSON(200, gin.H{"order": findOrder("1"), "limit": limit, "date": Day{}})
	}) // 闭包查询
	// apidoc:ignore
	group.GET("/internal", Missing)
}
