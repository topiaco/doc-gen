package parameters

import (
	"fixture/parameters/helper"

	"github.com/gin-gonic/gin"
)

// Register 注册参数分析测试路由
func Register(engine *gin.Engine) {
	group := engine.Group("/parameters")
	group.GET("/:id", ReadParameters)
	group.POST("/:unused", MissingPathParameter)
}

// ReadParameters 读取直接参数和配置 Helper 参数
func ReadParameters(c *gin.Context) {
	id := c.Param("id")
	required := c.Query("required")
	if required == "" {
		return
	}
	optional := c.DefaultQuery("optional", "default")
	header := c.GetHeader("X-Token")
	form := c.PostForm("name")
	file, _ := c.FormFile("upload")
	page := helper.QueryInt(c, "page")
	tags := helper.QueryArray(c, "tag")
	_, _, _, _, _, _, _, _ = id, required, optional, header, form, file, page, tags
}

// MissingPathParameter 保留未读取 Path 参数并使用动态 Query 名
func MissingPathParameter(c *gin.Context) {
	_ = c.Query(dynamicName())
}

// dynamicName 返回运行期参数名
func dynamicName() string {
	return "dynamic"
}
