package main

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

// Order 订单详情
// 用于展示订单金额和当前支付状态
type Order struct {
	// ID 订单唯一标识
	ID int64 `json:"id" gorm:"primaryKey"`
	// AmountCents 订单实付金额
	// 单位：分，已扣除优惠金额
	AmountCents  int64  `json:"amountCents"`
	Status       string `json:"status" gorm:"comment:订单状态：pending=待支付，paid=已支付"`
	Remark       string `json:"remark,omitempty"` // 订单备注；为空时不返回
	InternalNote string `json:"-"`                // 内部处理备注
}

// Response 接口响应
type Response[T any] struct {
	Data T `json:"data"` // 返回数据
}

// CreateOrderRequest 创建订单的请求参数
type CreateOrderRequest struct {
	CustomerID int64  `json:"customerId" binding:"required"` // 客户 ID
	Remark     string `json:"remark" binding:"max=200"`      // 订单备注
}

// Respond is an existing response helper; the generator follows its any argument
func Respond(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Response[any]{Data: data})
}

type OrderHandler struct{}

// Get 查询订单详情
// 返回订单金额和当前支付状态
func (OrderHandler) Get(c *gin.Context) {
	_ = c.Param("id")
	Respond(c, Order{ID: 1, AmountCents: 12900, Status: "paid"})
}

// Create 创建订单
func Create(c *gin.Context) {
	var request CreateOrderRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	Respond(c, Order{ID: 2, Status: "pending", Remark: request.Remark})
}

// Register 在指定路由组注册订单和健康检查接口
func Register(group *gin.RouterGroup) {
	group.GET("/orders/:id", OrderHandler{}.Get)
	group.POST("/orders", Create)
	group.GET("/health", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) }) // 健康检查
}

// main 创建 Gin 路由并启动示例服务
func main() {
	engine := gin.Default()
	Register(engine.Group("/api"))
	_ = engine.Run(":8080")
}
