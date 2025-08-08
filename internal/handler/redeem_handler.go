package handler

import (
	"net/http"
	"strconv"

	"wjdr-backend-go/internal/model"
	"wjdr-backend-go/internal/service"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// RedeemHandler 兑换处理器（与Node版本对齐）
type RedeemHandler struct {
	redeemService *service.RedeemService
	logger        *zap.Logger
}

func NewRedeemHandler(redeemService *service.RedeemService, logger *zap.Logger) *RedeemHandler {
	return &RedeemHandler{
		redeemService: redeemService,
		logger:        logger,
	}
}

// SubmitRedeemCode 提交新的兑换码（与Node版本对齐）
// POST /api/redeem
func (h *RedeemHandler) SubmitRedeemCode(c *gin.Context) {
	var request struct {
		Code   string `json:"code" binding:"required"`
		IsLong bool   `json:"is_long"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		ErrorResponse(c, http.StatusBadRequest, false, "兑换码不能为空")
		return
	}

	h.logger.Info("📝 收到提交兑换码请求",
		zap.String("code", request.Code),
		zap.Bool("is_long", request.IsLong))

	result, err := h.redeemService.SubmitRedeemCode(request.Code, request.IsLong)
	if err != nil {
		h.logger.Error("提交兑换码失败", zap.Error(err))
		ErrorResponse(c, http.StatusInternalServerError, false, "提交兑换码失败")
		return
	}

	if !result.Success {
		statusCode := http.StatusBadRequest
		if result.Error == "兑换码已存在" {
			statusCode = http.StatusConflict
		}

		response := gin.H{
			"success": false,
			"error":   result.Error,
		}

		if result.Data != nil {
			response["data"] = result.Data
		}

		c.JSON(statusCode, response)
		return
	}

	// 成功响应
	SuccessResponseWithMessage(c, result.Message, result.Data)
}

// GetAllRedeemCodes 获取兑换码列表（去除分页）
// GET /api/redeem
func (h *RedeemHandler) GetAllRedeemCodes(c *gin.Context) {
	result, err := h.redeemService.GetAllRedeemCodes()
	if err != nil {
		h.logger.Error("获取兑换码列表失败", zap.Error(err))
		ErrorResponse(c, http.StatusInternalServerError, false, "获取兑换码列表失败")
		return
	}

	if !result.Success {
		ErrorResponse(c, http.StatusInternalServerError, false, "获取兑换码列表失败")
		return
	}

	c.JSON(http.StatusOK, result)
}

// GetRedeemCodeDetails 获取兑换码详细信息（与Node版本对齐）
// GET /api/redeem/:id/details
func (h *RedeemHandler) GetRedeemCodeDetails(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		ErrorResponse(c, http.StatusBadRequest, false, "无效的兑换码ID")
		return
	}

	result, err := h.redeemService.GetRedeemCodeDetails(id)
	if err != nil {
		h.logger.Error("获取兑换码详情失败", zap.Error(err))
		ErrorResponse(c, http.StatusInternalServerError, false, "获取兑换码详情失败")
		return
	}

	if !result.Success {
		statusCode := http.StatusBadRequest
		if result.Error == "兑换码不存在" {
			statusCode = http.StatusNotFound
		}
		ErrorResponse(c, statusCode, false, result.Error)
		return
	}

	SuccessResponse(c, result.Data)
}

// GetRedeemCodeLogs 获取兑换码的兑换日志（与Node版本对齐）
// GET /api/redeem/:id/logs
func (h *RedeemHandler) GetRedeemCodeLogs(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		ErrorResponse(c, http.StatusBadRequest, false, "无效的兑换码ID")
		return
	}

	result, err := h.redeemService.GetRedeemCodeLogs(id)
	if err != nil {
		h.logger.Error("获取兑换日志失败", zap.Error(err))
		ErrorResponse(c, http.StatusInternalServerError, false, "获取兑换日志失败")
		return
	}

	if !result.Success {
		ErrorResponse(c, http.StatusInternalServerError, false, result.Error)
		return
	}

	SuccessResponse(c, result.Data)
}

// GetAllLogs 获取所有兑换日志（去除分页，保留result过滤）
// GET /api/redeem/logs
func (h *RedeemHandler) GetAllLogs(c *gin.Context) {
	// 支持通过查询参数 result=success|failed 过滤
	resultFilter := c.DefaultQuery("result", "")

	var (
		srvResp *model.APIResponse
		err     error
	)
	if resultFilter == "success" || resultFilter == "failed" || resultFilter == "" {
		if resultFilter == "" {
			srvResp, err = h.redeemService.GetAllLogs()
		} else {
			srvResp, err = h.redeemService.GetAllLogsFiltered(resultFilter)
		}
	} else {
		ErrorResponse(c, http.StatusBadRequest, false, "result 仅支持 success/failed 或留空")
		return
	}

	if err != nil {
		h.logger.Error("获取兑换日志失败", zap.Error(err))
		ErrorResponse(c, http.StatusInternalServerError, false, "获取兑换日志失败")
		return
	}

	if !srvResp.Success {
		ErrorResponse(c, http.StatusInternalServerError, false, srvResp.Error)
		return
	}

	logs := srvResp.Data.([]model.RedeemLog)
	successCount := 0
	failedCount := 0
	for _, log := range logs {
		if log.Result == "success" {
			successCount++
		} else {
			failedCount++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    logs,
		"stats": map[string]int{
			"total":   len(logs),
			"success": successCount,
			"failed":  failedCount,
		},
	})
}

// GetAccountsForRedeemCode 获取兑换码的账号处理状态（与Node版本对齐）
// GET /api/redeem/:id/accounts
func (h *RedeemHandler) GetAccountsForRedeemCode(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		ErrorResponse(c, http.StatusBadRequest, false, "无效的兑换码ID")
		return
	}

	result, err := h.redeemService.GetAccountsForRedeemCode(id)
	if err != nil {
		h.logger.Error("获取账号处理状态失败", zap.Error(err))
		ErrorResponse(c, http.StatusInternalServerError, false, "获取账号处理状态失败")
		return
	}

	if !result.Success {
		statusCode := http.StatusBadRequest
		if result.Error == "兑换码不存在" {
			statusCode = http.StatusNotFound
		}
		ErrorResponse(c, statusCode, false, result.Error)
		return
	}

	SuccessResponse(c, result.Data)
}

// DeleteRedeemCode 删除兑换码（与Node版本对齐）
// DELETE /api/redeem/:id
func (h *RedeemHandler) DeleteRedeemCode(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		ErrorResponse(c, http.StatusBadRequest, false, "无效的兑换码ID")
		return
	}

	h.logger.Info("🗑️ 收到删除兑换码请求", zap.Int("id", id))

	result, err := h.redeemService.DeleteRedeemCode(id)
	if err != nil {
		h.logger.Error("删除兑换码失败", zap.Error(err))
		ErrorResponse(c, http.StatusInternalServerError, false, "删除兑换码失败")
		return
	}

	if !result.Success {
		statusCode := http.StatusBadRequest
		if result.Error == "兑换码不存在" {
			statusCode = http.StatusNotFound
		}
		ErrorResponse(c, statusCode, false, result.Error)
		return
	}

	SuccessResponseWithMessage(c, result.Message, nil)
}

// BulkDeleteRedeemCodes 批量删除兑换码（与Node版本对齐）
// DELETE /api/redeem
func (h *RedeemHandler) BulkDeleteRedeemCodes(c *gin.Context) {
	var request struct {
		IDs []int `json:"ids" binding:"required"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		ErrorResponse(c, http.StatusBadRequest, false, "请提供要删除的兑换码ID列表")
		return
	}

	if len(request.IDs) == 0 {
		ErrorResponse(c, http.StatusBadRequest, false, "没有指定要删除的兑换码")
		return
	}

	h.logger.Info("🗑️ 收到批量删除兑换码请求", zap.Int("count", len(request.IDs)))

	result, err := h.redeemService.BulkDeleteRedeemCodes(request.IDs)
	if err != nil {
		h.logger.Error("批量删除兑换码失败", zap.Error(err))
		ErrorResponse(c, http.StatusInternalServerError, false, "批量删除兑换码失败")
		return
	}

	if !result.Success {
		ErrorResponse(c, http.StatusBadRequest, false, result.Error)
		return
	}

	SuccessResponseWithMessage(c, result.Message, result.Data)
}

// RetryRedeemCode 重试兑换码（与Node版本对齐）
// POST /api/redeem/:id/retry
func (h *RedeemHandler) RetryRedeemCode(c *gin.Context) {
	// 支持两种方式：
	// 1) 兼容旧路径 /redeem/:id/retry（优先级高，路径参数存在时按单个处理）
	// 2) 新的JSON Body: { ids: number[] }（当无路径id或提供ids时，按批量处理）

	idStr := c.Param("id")
	if idStr != "" {
		id, err := strconv.Atoi(idStr)
		if err != nil {
			ErrorResponse(c, http.StatusBadRequest, false, "无效的兑换码ID")
			return
		}
		// 将单个id也按批量接口走，统一风格
		h.logger.Info("🔄 收到重试兑换码请求(单个)", zap.Int("id", id))
		result, err := h.redeemService.RetryRedeemCodes([]int{id})
		if err != nil {
			h.logger.Error("重试兑换码失败", zap.Error(err))
			ErrorResponse(c, http.StatusInternalServerError, false, "重试兑换码失败")
			return
		}
		if !result.Success {
			statusCode := http.StatusBadRequest
			if result.Error == "兑换码不存在" {
				statusCode = http.StatusNotFound
			}
			ErrorResponse(c, statusCode, false, result.Error)
			return
		}
		SuccessResponseWithMessage(c, result.Message, result.Data)
		return
	}

	// Body 批量（或单个）
	var req struct {
		IDs []int `json:"ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.IDs) == 0 {
		ErrorResponse(c, http.StatusBadRequest, false, "请提供要补充兑换的兑换码ID数组")
		return
	}
	h.logger.Info("🔄 收到批量重试兑换码请求", zap.Int("count", len(req.IDs)))
	result, err := h.redeemService.RetryRedeemCodes(req.IDs)
	if err != nil {
		h.logger.Error("批量重试兑换码失败", zap.Error(err))
		ErrorResponse(c, http.StatusInternalServerError, false, "批量重试兑换码失败")
		return
	}
	if !result.Success {
		ErrorResponse(c, http.StatusBadRequest, false, result.Error)
		return
	}
	SuccessResponseWithMessage(c, result.Message, result.Data)
}

// RegisterRedeemRoutes 注册兑换相关路由（与Node版本对齐）
func (h *RedeemHandler) RegisterRoutes(router *gin.RouterGroup, authMiddleware gin.HandlerFunc) {
	redeem := router.Group("/redeem")
	{
		// 提交新的兑换码（需要管理员权限）
		redeem.POST("", authMiddleware, h.SubmitRedeemCode)

		// 获取兑换码列表（无需认证）
		redeem.GET("", h.GetAllRedeemCodes)

		// 获取兑换码详细信息（无需认证）
		redeem.GET("/:id/details", h.GetRedeemCodeDetails)

		// 获取兑换码的兑换日志（无需认证）
		redeem.GET("/:id/logs", h.GetRedeemCodeLogs)

		// 获取兑换码的账号处理状态（无需认证）
		redeem.GET("/:id/accounts", h.GetAccountsForRedeemCode)

		// 获取所有兑换日志（无需认证）
		redeem.GET("/logs", h.GetAllLogs)

		// 删除单个兑换码（需要管理员权限）
		redeem.DELETE("/:id", authMiddleware, h.DeleteRedeemCode)

		// 批量删除兑换码（需要管理员权限）
		redeem.DELETE("", authMiddleware, h.BulkDeleteRedeemCodes)

		// 重试兑换码（需要管理员权限）
		// 新风格统一入口：POST /api/redeem/retry，Body: {"ids": [1,2,...]}
		redeem.POST("/retry", authMiddleware, h.RetryRedeemCode)
		redeem.POST("/:id/retry", authMiddleware, h.RetryRedeemCode)
	}
}
