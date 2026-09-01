package handlers

import "github.com/gin-gonic/gin"

// Register 注册 Handler 定位测试路由
func Register(engine *gin.Engine) {
	group := engine.Group("/handlers")
	group.GET("/exact", Exact)

	dynamic := selectHandler()
	group.GET("/dynamic", dynamic)
	group.GET("/closure", func(*gin.Context) {})
}

// Exact 提供可精确定位的 Handler
func Exact(*gin.Context) {}

// selectHandler 模拟运行期选择 Handler
func selectHandler() gin.HandlerFunc {
	return Exact
}
