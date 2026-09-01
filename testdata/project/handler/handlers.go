package handler

import (
	"example/catalog/model"
	"example/catalog/respond"

	"github.com/gin-gonic/gin"
)

// Get 查询单个条目
func Get(c *gin.Context) {
	_ = c.Param("id")
	respond.OK(c, &model.Item{})
}

// Create 创建条目
func Create(c *gin.Context) {
	request := &model.CreateItemRequest{}
	_ = c.ShouldBindJSON(request)
	respond.OK(c, &model.Item{})
}
