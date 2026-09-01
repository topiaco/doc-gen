package gin

// Context 模拟 Gin 请求上下文
type Context struct{}

// Param 读取 Path 参数
func (*Context) Param(string) string { return "" }

// Query 读取 Query 参数
func (*Context) Query(string) string { return "" }

// DefaultQuery 读取带默认值的 Query 参数
func (*Context) DefaultQuery(string, string) string { return "" }

// GetHeader 读取 Header 参数
func (*Context) GetHeader(string) string { return "" }

// PostForm 读取 Form 参数
func (*Context) PostForm(string) string { return "" }

// FormFile 读取上传文件
func (*Context) FormFile(string) (any, error) { return nil, nil }

// BindJSON 绑定 JSON 请求体
func (*Context) BindJSON(any) error { return nil }

// ShouldBindJSON 绑定 JSON 请求体
func (*Context) ShouldBindJSON(any) error { return nil }

// JSON 写入 JSON 响应
func (*Context) JSON(int, any) {}

// Data 写入字节响应
func (*Context) Data(int, string, []byte) {}

// File 写入文件响应
func (*Context) File(string) {}

// FileAttachment 写入附件响应
func (*Context) FileAttachment(string, string) {}

// Redirect 写入重定向响应
func (*Context) Redirect(int, string) {}

// HandlerFunc 模拟 Gin Handler
type HandlerFunc func(*Context)

// Engine 模拟 Gin 路由引擎
type Engine struct{}

// RouterGroup 模拟 Gin 路由组
type RouterGroup struct{}

// Group 创建 Engine 下的路由组
func (*Engine) Group(string, ...HandlerFunc) *RouterGroup {
	return &RouterGroup{}
}

// Group 创建嵌套路由组
func (*RouterGroup) Group(string, ...HandlerFunc) *RouterGroup {
	return &RouterGroup{}
}

// GET 注册 GET 路由
func (*RouterGroup) GET(string, ...HandlerFunc) {}

// POST 注册 POST 路由
func (*RouterGroup) POST(string, ...HandlerFunc) {}

// PUT 注册 PUT 路由
func (*RouterGroup) PUT(string, ...HandlerFunc) {}

// DELETE 注册 DELETE 路由
func (*RouterGroup) DELETE(string, ...HandlerFunc) {}
