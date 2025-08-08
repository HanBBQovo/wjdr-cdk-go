package handler

import (
	"net/http"
	"strconv"

	"wjdr-backend-go/internal/service"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// AccountHandler 账号处理器（与Node版本对齐）
type AccountHandler struct {
	accountService *service.AccountService
	logger         *zap.Logger
}

func NewAccountHandler(accountService *service.AccountService, logger *zap.Logger) *AccountHandler {
	return &AccountHandler{
		accountService: accountService,
		logger:         logger,
	}
}

// GetAllAccounts 获取所有账号（与Node版本对齐）
// GET /api/accounts
func (h *AccountHandler) GetAllAccounts(c *gin.Context) {
	accounts, err := h.accountService.GetAllAccounts()
	if err != nil {
		h.logger.Error("获取账号列表错误", zap.Error(err))
		ErrorResponse(c, http.StatusInternalServerError, false, "获取账号列表失败")
		return
	}

	SuccessResponse(c, accounts)
}

// CreateAccount 添加新账号（与Node版本对齐）
// POST /api/accounts
func (h *AccountHandler) CreateAccount(c *gin.Context) {
	var request struct {
		FID string `json:"fid" binding:"required"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		ErrorResponse(c, http.StatusBadRequest, false, "FID不能为空")
		return
	}

	h.logger.Info("📝 收到添加账号请求", zap.String("fid", request.FID))

	result, err := h.accountService.CreateAccount(request.FID)
	if err != nil {
		h.logger.Error("添加账号失败", zap.Error(err))
		ErrorResponse(c, http.StatusInternalServerError, false, "添加账号失败")
		return
	}

	// 根据结果返回相应的状态码和响应
	if !result.Success {
		statusCode := http.StatusBadRequest
		if result.Error == "账号已存在" {
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

// VerifyAccount 手动验证账号（与Node版本对齐）
// POST /api/accounts/:id/verify
func (h *AccountHandler) VerifyAccount(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		ErrorResponse(c, http.StatusBadRequest, false, "无效的账号ID")
		return
	}

	h.logger.Info("🔍 收到手动验证账号请求", zap.Int("id", id))

	result, err := h.accountService.VerifyAccount(id)
	if err != nil {
		h.logger.Error("验证账号失败", zap.Error(err))
		ErrorResponse(c, http.StatusInternalServerError, false, "验证账号失败")
		return
	}

	if !result.Success {
		ErrorResponse(c, http.StatusBadRequest, false, result.Error)
		return
	}

	SuccessResponseWithMessage(c, result.Message, result.Data)
}

// DeleteAccount 删除账号（与Node版本对齐）
// DELETE /api/accounts/:id
func (h *AccountHandler) DeleteAccount(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		ErrorResponse(c, http.StatusBadRequest, false, "无效的账号ID")
		return
	}

	h.logger.Info("🗑️ 收到删除账号请求", zap.Int("id", id))

	result, err := h.accountService.DeleteAccount(id)
	if err != nil {
		h.logger.Error("删除账号失败", zap.Error(err))
		ErrorResponse(c, http.StatusInternalServerError, false, "删除账号失败")
		return
	}

	if !result.Success {
		statusCode := http.StatusBadRequest
		if result.Error == "账号不存在" {
			statusCode = http.StatusNotFound
		}

		ErrorResponse(c, statusCode, false, result.Error)
		return
	}

	SuccessResponseWithMessage(c, result.Message, nil)
}

// FixAllStats 修复所有兑换码统计（与Node版本对齐）
// POST /api/accounts/fix-stats
func (h *AccountHandler) FixAllStats(c *gin.Context) {
	h.logger.Info("🔧 收到修复统计请求")

	result, err := h.accountService.FixAllStats()
	if err != nil {
		h.logger.Error("修复统计失败", zap.Error(err))
		ErrorResponse(c, http.StatusInternalServerError, false, "修复统计失败")
		return
	}

	if !result.Success {
		ErrorResponse(c, http.StatusInternalServerError, false, result.Error)
		return
	}

	SuccessResponseWithMessage(c, result.Message, result.Data)
}

// RegisterAccountRoutes 注册账号相关路由（与Node版本对齐）
func (h *AccountHandler) RegisterRoutes(router *gin.RouterGroup, authMiddleware gin.HandlerFunc) {
	accounts := router.Group("/accounts")
	{
		// 获取所有账号（无需认证）
		accounts.GET("", h.GetAllAccounts)

		// 添加新账号（无需认证）
		accounts.POST("", h.CreateAccount)

		// 手动验证账号（无需认证）
		accounts.POST("/:id/verify", h.VerifyAccount)

		// 删除账号（需要管理员权限）
		accounts.DELETE("/:id", authMiddleware, h.DeleteAccount)

		// 修复统计（需要管理员权限）
		accounts.POST("/fix-stats", authMiddleware, h.FixAllStats)
	}
}

