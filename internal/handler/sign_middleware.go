package handler

import (
	"net/http"
	"wjdr-backend-go/internal/utils"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// SignVerificationMiddleware 签名验证中间件（专用于添加账号接口）
func SignVerificationMiddleware(salt string, logger *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 解析请求体
		var request struct {
			FID       string `json:"fid"`
			Timestamp string `json:"timestamp"`
			Sign      string `json:"sign"`
		}

		// 绑定JSON请求体
		if err := c.ShouldBindJSON(&request); err != nil {
			logger.Warn("签名验证：请求格式错误", zap.Error(err))
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "请求格式错误，缺少必要的签名参数",
			})
			c.Abort()
			return
		}

		// 检查必要参数
		if request.FID == "" || request.Timestamp == "" || request.Sign == "" {
			logger.Warn("签名验证：缺少必要参数",
				zap.String("fid", request.FID),
				zap.String("timestamp", request.Timestamp),
				zap.String("sign", request.Sign))
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "FID、时间戳和签名不能为空",
			})
			c.Abort()
			return
		}

		// 验证签名
		if !utils.VerifyAccountSign(request.FID, request.Timestamp, request.Sign, salt) {
			logger.Warn("签名验证失败",
				zap.String("fid", request.FID),
				zap.String("timestamp", request.Timestamp),
				zap.String("provided_sign", request.Sign))
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"error":   "签名验证失败",
			})
			c.Abort()
			return
		}

		logger.Info("签名验证成功",
			zap.String("fid", request.FID),
			zap.String("timestamp", request.Timestamp))

		// 将验证通过的参数存储到Context中，供后续handler使用
		c.Set("verified_fid", request.FID)
		c.Set("verified_timestamp", request.Timestamp)
		c.Set("verified_sign", request.Sign)

		// 验证通过，继续处理
		c.Next()
	}
}
