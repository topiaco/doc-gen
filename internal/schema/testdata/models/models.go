package models

import "time"

// Base 表示嵌入的基础字段
type Base struct {
	ID uint `json:"id" description:"记录标识" binding:"required,min=1"`
}

// Detail 表示嵌套详情
type Detail struct {
	Name string `json:"name" gorm:"comment:详情名称"`
}

// ProductItem 表示产品列表项
type ProductItem struct {
	Base
	OccurredAt *time.Time `json:"occurred_at"`                                                       // 发生时间
	Detail     *Detail    `json:"detail" binding:"required"`                                         // 详情
	Explicit   string     `json:"explicit" description:"显式说明" binding:"min=2" gorm:"comment:GORM说明"` // 行尾说明
	Comment    string     `json:"comment" gorm:"comment:GORM说明"`                                     // 行尾说明
	GormOnly   string     `json:"gorm_only" gorm:"comment:GORM说明"`
	Optional   string     `json:"optional,omitempty" binding:"required"`
}

// Pagination 表示泛型分页
type Pagination[T any] struct {
	Items []T `json:"items" binding:"required"`
}

// Node 表示循环节点
type Node struct {
	Children []*Node `json:"children"`
}
