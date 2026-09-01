package routes

import (
	"example/catalog/handler"

	"github.com/gin-gonic/gin"
)

// Register 注册目录接口
func Register(engine *gin.Engine) {
	group := engine.Group("/catalog")
	group.GET("/:id", handler.Get) // 查询条目
	group.POST("", handler.Create) // 创建条目
}
