// Package helper provides parameter reader fixtures
package helper

import "github.com/gin-gonic/gin"

// QueryInt 读取整数 Query 参数
func QueryInt(*gin.Context, string) int { return 0 }

// QueryArray 读取数组 Query 参数
func QueryArray(*gin.Context, string) []string { return nil }
