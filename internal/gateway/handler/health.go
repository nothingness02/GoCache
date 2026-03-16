package handler

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

// HealthHandler 专门处理健康检查相关的请求
type HealthHandler struct{}

func NewHealthHandler() *HealthHandler {
	return &HealthHandler{}
}

// Ping 是最简单的探针接口
func(h *HealthHandler) Ping(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"message": "pong",
		"system": "Go-AI-KV-Gateway",
	})
}