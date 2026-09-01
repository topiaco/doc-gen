package routes

import "github.com/gin-gonic/gin"

const apiPrefix = "/api"
const itemPath = "/items"

// Register 注册静态和动态测试路由
func Register(engine *gin.Engine) {
	v1 := engine.Group(apiPrefix).Group("/v1")
	v1.GET(itemPath+"/:id", GetItem) // 查询项目
	v1.POST(itemPath, CreateItem)
	engine.Group("/admin").DELETE("/items/:id", DeleteItem)

	dynamicGroup := engine.Group(dynamicPath())
	dynamicGroup.PUT("/items/:id", UpdateItem)
	v1.PUT(dynamicPath(), UpdateItem)
}

// GetItem 返回单个项目
func GetItem(*gin.Context) {}

// CreateItem 创建项目
func CreateItem(*gin.Context) {}

// UpdateItem 更新项目
func UpdateItem(*gin.Context) {}

// DeleteItem 删除项目
func DeleteItem(*gin.Context) {}

// dynamicPath 返回运行期路径
func dynamicPath() string {
	return "/dynamic"
}
