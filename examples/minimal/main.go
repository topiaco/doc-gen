package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Item 是示例响应对象。
type Item struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Respond 写入示例项目的统一 JSON 响应。
func Respond(c *gin.Context, value any) {
	c.JSON(http.StatusOK, gin.H{"status": "ok", "payload": value})
}

func getItem(c *gin.Context) {
	id := c.Param("id")
	Respond(c, &Item{ID: id, Name: "Example"})
}

func main() {
	engine := gin.Default()
	engine.GET("/items/:id", getItem) // Get one item
	_ = engine.Run(":8080")
}
