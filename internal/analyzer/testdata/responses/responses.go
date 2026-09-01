package responses

import (
	"errors"

	"github.com/gin-gonic/gin"
)

// Item 表示响应对象
type Item struct {
	ID uint `json:"id"`
}

// Pagination 表示泛型分页响应
type Pagination[T any] struct {
	Items []T `json:"items"`
}

// Register 注册统一响应分析测试路由
func Register(engine *gin.Engine) {
	group := engine.Group("/responses")
	group.GET("/shapes", Shapes)
	group.GET("/merge", Merge)
	group.GET("/statuses", Statuses)
	group.GET("/dynamic-status", DynamicStatus)
	group.GET("/json", DirectJSON)
	group.GET("/pdf", PDF)
	group.GET("/excel", Excel)
	group.GET("/file", File)
	group.GET("/attachment", Attachment)
	group.GET("/redirect", Redirect)
	group.GET("/binary-helper", BinaryHelper)
}

// Shapes 返回不同静态类型的成功数据
func Shapes(c *gin.Context) {
	SetResponseOK(c, &Item{})
	SetResponseOK(c, []*Item{})
	SetResponseOK(c, &Pagination[*Item]{})
	SetResponseOK(c, uint(1))
	SetResponseOK(c, nil)
	SetResponseOK(c, map[string]any{"item": &Item{}})
}

// Merge 返回同一状态码的不同包装
func Merge(c *gin.Context) {
	SetResponseOK(c, &Item{})
	SetResponseError(c, 200, errors.New("failed"))
	SetResponseBusinessError(c, "failed")
}

// Statuses 返回不同错误状态码
func Statuses(c *gin.Context) {
	SetResponseError(c, 400, errors.New("bad request"))
	SetResponseError(c, 500, errors.New("internal error"))
}

// DynamicStatus 返回运行期状态码
func DynamicStatus(c *gin.Context) {
	SetResponseError(c, runtimeStatus(), errors.New("failed"))
}

// runtimeStatus 模拟运行期状态码
func runtimeStatus() int {
	return 500
}

// DirectJSON 直接返回 JSON
func DirectJSON(c *gin.Context) {
	c.JSON(201, &Item{})
}

// PDF 按条件返回 PDF 或 JSON
func PDF(c *gin.Context) {
	if runtimeCondition() {
		c.Data(200, "application/pdf", []byte("pdf"))
		return
	}
	c.JSON(200, map[string]string{"url": "/report.pdf"})
}

// Excel 返回 Excel 文件
func Excel(c *gin.Context) {
	c.Data(200, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", []byte("excel"))
}

// File 返回普通文件
func File(c *gin.Context) {
	c.File("/tmp/report.bin")
}

// Attachment 返回下载附件
func Attachment(c *gin.Context) {
	c.FileAttachment("/tmp/report.bin", "report.bin")
}

// Redirect 返回重定向
func Redirect(c *gin.Context) {
	c.Redirect(302, "/target")
}

// BinaryHelper 通过配置 Helper 返回文件
func BinaryHelper(c *gin.Context) {
	SendBinary(c, []byte("binary"))
}

// runtimeCondition 模拟运行期响应分支
func runtimeCondition() bool {
	return true
}

// SetResponseOK 模拟统一成功响应
func SetResponseOK(*gin.Context, any) {}

// SetResponseError 模拟统一错误响应
func SetResponseError(*gin.Context, int, error) {}

// SetResponseBusinessError 模拟统一业务错误响应
func SetResponseBusinessError(*gin.Context, string) {}

// SendBinary 模拟配置化二进制响应 Helper
func SendBinary(*gin.Context, []byte) {}
