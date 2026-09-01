package model

// Item 表示目录条目
type Item struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
}

// CreateItemRequest 表示创建请求
type CreateItemRequest struct {
	Name string `json:"name" binding:"required"`
}
