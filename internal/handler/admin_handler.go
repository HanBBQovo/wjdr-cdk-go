package handler

import (
	"net/http"
	"strconv"

	"wjdr-backend-go/internal/service"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// AdminHandler 管理员处理器（与Node版本对齐）
type AdminHandler struct {
	adminService *service.AdminService
	logger       *zap.Logger
}

func NewAdminHandler(adminService *service.AdminService, logger *zap.Logger) *AdminHandler {
	return &AdminHandler{
		adminService: adminService,
		logger:       logger,
	}
}

// VerifyPassword 验证管理员密码（与Node版本对齐）
// POST /api/admin/verify
func (h *AdminHandler) VerifyPassword(c *gin.Context) {
	var request struct {
		Password string `json:"password" binding:"required"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		ErrorResponse(c, http.StatusBadRequest, false, "密码不能为空")
		return
	}

	h.logger.Info("🔐 收到管理员密码验证请求")

	result, err := h.adminService.VerifyPassword(request.Password)
	if err != nil {
		h.logger.Error("密码验证失败", zap.Error(err))
		ErrorResponse(c, http.StatusInternalServerError, false, "密码验证失败")
		return
	}

	if !result.Success {
		ErrorResponse(c, http.StatusUnauthorized, false, result.Error)
		return
	}

	// 成功响应
	SuccessResponseWithMessage(c, result.Message, result.Data)
}

// VerifyToken 验证Token有效性（与Node版本对齐）
// GET /api/admin/verify-token
func (h *AdminHandler) VerifyToken(c *gin.Context) {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		ErrorResponse(c, http.StatusBadRequest, false, "需要提供 Authorization 头")
		return
	}

	// 提取token
	tokenParts := c.Request.Header.Get("Authorization")
	if len(tokenParts) < 7 || tokenParts[:7] != "Bearer " {
		ErrorResponse(c, http.StatusBadRequest, false, "Authorization 头格式错误")
		return
	}

	token := tokenParts[7:]

	result, err := h.adminService.VerifyToken(token)
	if err != nil {
		h.logger.Error("Token验证失败", zap.Error(err))
		ErrorResponse(c, http.StatusInternalServerError, false, "Token验证失败")
		return
	}

	if !result.Success {
		ErrorResponse(c, http.StatusUnauthorized, false, result.Error)
		return
	}

	SuccessResponseWithMessage(c, result.Message, nil)
}

// GetAllPasswords 获取所有管理员密码信息（与Node版本对齐）
// GET /api/admin/passwords
func (h *AdminHandler) GetAllPasswords(c *gin.Context) {
	h.logger.Info("📋 收到获取密码列表请求")

	result, err := h.adminService.GetAllPasswords()
	if err != nil {
		h.logger.Error("获取密码列表失败", zap.Error(err))
		ErrorResponse(c, http.StatusInternalServerError, false, "获取密码列表失败")
		return
	}

	if !result.Success {
		ErrorResponse(c, http.StatusInternalServerError, false, result.Error)
		return
	}

	SuccessResponse(c, result.Data)
}

// CreatePassword 创建新的管理员密码（与Node版本对齐）
// POST /api/admin/passwords
func (h *AdminHandler) CreatePassword(c *gin.Context) {
	var request struct {
		Password    string `json:"password" binding:"required"`
		Description string `json:"description"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		ErrorResponse(c, http.StatusBadRequest, false, "密码不能为空")
		return
	}

	h.logger.Info("🔑 收到创建密码请求", zap.String("description", request.Description))

	result, err := h.adminService.CreatePassword(request.Password, request.Description)
	if err != nil {
		h.logger.Error("创建密码失败", zap.Error(err))
		ErrorResponse(c, http.StatusInternalServerError, false, "创建密码失败")
		return
	}

	if !result.Success {
		ErrorResponse(c, http.StatusBadRequest, false, result.Error)
		return
	}

	SuccessResponseWithMessage(c, result.Message, result.Data)
}

// DeletePassword 删除管理员密码（与Node版本对齐）
// DELETE /api/admin/passwords/:id
func (h *AdminHandler) DeletePassword(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		ErrorResponse(c, http.StatusBadRequest, false, "无效的密码ID")
		return
	}

	h.logger.Info("🗑️ 收到删除密码请求", zap.Int("id", id))

	result, err := h.adminService.DeletePassword(id)
	if err != nil {
		h.logger.Error("删除密码失败", zap.Error(err))
		ErrorResponse(c, http.StatusBadRequest, false, result.Error)
		return
	}

	if !result.Success {
		ErrorResponse(c, http.StatusBadRequest, false, result.Error)
		return
	}

	SuccessResponseWithMessage(c, result.Message, nil)
}

// UpdatePasswordStatus 更新密码状态（与Node版本对齐）
// PUT /api/admin/passwords/:id/status
func (h *AdminHandler) UpdatePasswordStatus(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		ErrorResponse(c, http.StatusBadRequest, false, "无效的密码ID")
		return
	}

	var request struct {
		IsActive bool `json:"is_active"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		ErrorResponse(c, http.StatusBadRequest, false, "请求参数错误")
		return
	}

	h.logger.Info("🔄 收到更新密码状态请求",
		zap.Int("id", id),
		zap.Bool("is_active", request.IsActive))

	result, err := h.adminService.UpdatePasswordStatus(id, request.IsActive)
	if err != nil {
		h.logger.Error("更新密码状态失败", zap.Error(err))
		ErrorResponse(c, http.StatusInternalServerError, false, "更新密码状态失败")
		return
	}

	if !result.Success {
		ErrorResponse(c, http.StatusBadRequest, false, result.Error)
		return
	}

	SuccessResponseWithMessage(c, result.Message, nil)
}

// UpdateDefaultPassword 更新默认密码（与Node版本对齐）
// PUT /api/admin/passwords/:id
func (h *AdminHandler) UpdateDefaultPassword(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		ErrorResponse(c, http.StatusBadRequest, false, "无效的密码ID")
		return
	}

	if id != 1 {
		ErrorResponse(c, http.StatusBadRequest, false, "只能更新默认密码")
		return
	}

	var request struct {
		Password    string `json:"password" binding:"required"`
		Description string `json:"description"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		ErrorResponse(c, http.StatusBadRequest, false, "新密码不能为空")
		return
	}

	h.logger.Info("🔑 收到更新默认密码请求")

	result, err := h.adminService.UpdateDefaultPassword(request.Password, request.Description)
	if err != nil {
		h.logger.Error("更新默认密码失败", zap.Error(err))
		ErrorResponse(c, http.StatusBadRequest, false, result.Error)
		return
	}

	if !result.Success {
		ErrorResponse(c, http.StatusBadRequest, false, result.Error)
		return
	}

	SuccessResponseWithMessage(c, result.Message, nil)
}

// RegisterAdminRoutes 注册管理员相关路由（与Node版本对齐）
func (h *AdminHandler) RegisterRoutes(router *gin.RouterGroup, authMiddleware gin.HandlerFunc) {
	admin := router.Group("/admin")
	{
		// 验证密码（无需认证）
		admin.POST("/verify", h.VerifyPassword)

		// 验证Token（无需认证，但需要在头部提供token）
		admin.GET("/verify-token", h.VerifyToken)

		// 以下所有接口都需要管理员权限
		passwords := admin.Group("/passwords", authMiddleware)
		{
			// 获取所有密码信息
			passwords.GET("", h.GetAllPasswords)

			// 创建新密码
			passwords.POST("", h.CreatePassword)

			// 删除密码
			passwords.DELETE("/:id", h.DeletePassword)

			// 更新密码状态
			passwords.PUT("/:id/status", h.UpdatePasswordStatus)

			// 更新默认密码
			passwords.PUT("/:id", h.UpdateDefaultPassword)
		}

		// 统计修复接口（需要管理员权限）
		stats := admin.Group("/stats", authMiddleware)
		{
			// 修复所有兑换码统计
			stats.POST("/fix", func(c *gin.Context) {
				result, err := h.adminService.AccountService.FixAllStats()
				if err != nil {
					ErrorResponse(c, http.StatusInternalServerError, false, "修复统计失败")
					return
				}
				if !result.Success {
					ErrorResponse(c, http.StatusBadRequest, false, result.Error)
					return
				}
				SuccessResponseWithMessage(c, result.Message, result.Data)
			})
		}
	}
}
