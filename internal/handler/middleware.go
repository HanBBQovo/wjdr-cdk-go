package handler

import (
	"net/http"
	"strings"

	"wjdr-backend-go/internal/service"

	"github.com/gin-gonic/gin"
)

// CORSMiddleware CORS中间件（与Node版本对齐）
func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

// AuthMiddleware Token验证中间件（与Node版本对齐）
func AuthMiddleware(adminService *service.AdminService) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"error":   "需要提供 Authorization 头",
			})
			c.Abort()
			return
		}

		// 提取Bearer token
		tokenParts := strings.Split(authHeader, " ")
		if len(tokenParts) != 2 || tokenParts[0] != "Bearer" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"error":   "Authorization 头格式错误",
			})
			c.Abort()
			return
		}

		token := tokenParts[1]

		// 验证token
		result, err := adminService.VerifyToken(token)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"error":   "Token验证失败",
			})
			c.Abort()
			return
		}

		if !result.Success {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"error":   "Token无效或已过期",
			})
			c.Abort()
			return
		}

		// 将token存储到上下文中，供后续使用
		c.Set("token", token)
		c.Next()
	}
}

// ErrorResponse 统一错误响应格式（与Node版本对齐）
func ErrorResponse(c *gin.Context, statusCode int, success bool, error string) {
	c.JSON(statusCode, gin.H{
		"success": success,
		"error":   error,
	})
}

// SuccessResponse 统一成功响应格式（与Node版本对齐）
func SuccessResponse(c *gin.Context, data interface{}) {
	response := gin.H{
		"success": true,
	}

	if data != nil {
		response["data"] = data
	}

	c.JSON(http.StatusOK, response)
}

// SuccessResponseWithMessage 带消息的成功响应（与Node版本对齐）
func SuccessResponseWithMessage(c *gin.Context, message string, data interface{}) {
	response := gin.H{
		"success": true,
		"message": message,
	}

	if data != nil {
		response["data"] = data
	}

	c.JSON(http.StatusOK, response)
}

