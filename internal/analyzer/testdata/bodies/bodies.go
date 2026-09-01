package bodies

import "github.com/gin-gonic/gin"

// ObjectRequest 表示具名对象请求
type ObjectRequest struct {
	Name string `json:"name" binding:"required"`
	Code string `json:"code" validate:"required"`
	Note string `json:"note,omitempty"`
}

// Register 注册 JSON Body 分析测试路由
func Register(engine *gin.Engine) {
	group := engine.Group("/bodies")
	group.POST("/object", Object)
	group.POST("/array", Array)
	group.POST("/anonymous", Anonymous)
	group.POST("/dynamic", Dynamic)
}

// Object 绑定具名对象
func Object(c *gin.Context) {
	request := &ObjectRequest{}
	_ = c.BindJSON(request)
}

// Array 绑定对象数组
func Array(c *gin.Context) {
	request := make([]*ObjectRequest, 0)
	_ = c.ShouldBindJSON(&request)
}

// Anonymous 绑定匿名对象
func Anonymous(c *gin.Context) {
	var request struct {
		ID     uint     `json:"id" binding:"required"`
		Labels []string `json:"labels"`
	}
	_ = c.ShouldBindJSON(&request)
}

// Dynamic 绑定运行期类型
func Dynamic(c *gin.Context) {
	_ = c.BindJSON(dynamicBody())
}

// dynamicBody 返回动态请求对象
func dynamicBody() any {
	return nil
}
