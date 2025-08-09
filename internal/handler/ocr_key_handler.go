package handler

import (
	"net/http"
	"strconv"
	"wjdr-backend-go/internal/model"
	"wjdr-backend-go/internal/service"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// OCRKeyHandler 管理端 Key 接口
type OCRKeyHandler struct {
	svc    *service.OCRKeyService
	logger *zap.Logger
	// reload 回调：更新 OCRKeyManager（由 main 注入）
	reload func() error
}

func NewOCRKeyHandler(svc *service.OCRKeyService, logger *zap.Logger, reload func() error) *OCRKeyHandler {
	return &OCRKeyHandler{svc: svc, logger: logger, reload: reload}
}

// List 列出全部 Key（脱敏）
func (h *OCRKeyHandler) List(c *gin.Context) {
	keys, err := h.svc.ListAll()
	if err != nil {
		ErrorResponse(c, http.StatusInternalServerError, false, "获取OCR Keys失败")
		return
	}
	type item struct {
		ID             int    `json:"id"`
		Provider       string `json:"provider"`
		Name           string `json:"name"`
		APIKeyEnd      string `json:"apiKeyEnd"`
		IsActive       bool   `json:"isActive"`
		HasQuota       bool   `json:"hasQuota"`
		MonthlyQuota   int    `json:"monthlyQuota"`
		RemainingQuota int    `json:"remainingQuota"`
		Weight         int    `json:"weight"`
		Success        int    `json:"successCount"`
		Fail           int    `json:"failCount"`
	}
	resp := make([]item, 0, len(keys))
	for _, k := range keys {
		end := ""
		if n := len(k.APIKey); n >= 4 {
			end = k.APIKey[n-4:]
		}
		resp = append(resp, item{
			ID:             k.ID,
			Provider:       k.Provider,
			Name:           k.Name,
			APIKeyEnd:      end,
			IsActive:       k.IsActive,
			HasQuota:       k.HasQuota,
			MonthlyQuota:   k.MonthlyQuota,
			RemainingQuota: k.RemainingQuota,
			Weight:         k.Weight,
			Success:        k.SuccessCount,
			Fail:           k.FailCount,
		})
	}
	SuccessResponse(c, resp)
}

// Create 新增 Key
func (h *OCRKeyHandler) Create(c *gin.Context) {
	var req struct {
		Provider       string `json:"provider"`
		Name           string `json:"name" binding:"required"`
		APIKey         string `json:"apiKey" binding:"required"`
		SecretKey      string `json:"secretKey" binding:"required"`
		IsActive       *bool  `json:"isActive"`
		HasQuota       *bool  `json:"hasQuota"`
		Weight         *int   `json:"weight"`
		MonthlyQuota   *int   `json:"monthlyQuota"`
		RemainingQuota *int   `json:"remainingQuota"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorResponse(c, http.StatusBadRequest, false, "参数错误")
		return
	}
	k := model.OCRKey{
		Provider:  req.Provider,
		Name:      req.Name,
		APIKey:    req.APIKey,
		SecretKey: req.SecretKey,
		IsActive:  true,
		HasQuota:  true,
		Weight:    1,
	}
	if req.MonthlyQuota != nil {
		k.MonthlyQuota = *req.MonthlyQuota
	}
	if req.RemainingQuota != nil {
		k.RemainingQuota = *req.RemainingQuota
	} else {
		k.RemainingQuota = k.MonthlyQuota
	}
	if req.IsActive != nil {
		k.IsActive = *req.IsActive
	}
	if req.HasQuota != nil {
		k.HasQuota = *req.HasQuota
	}
	if req.Weight != nil && *req.Weight > 0 {
		k.Weight = *req.Weight
	}

	id, err := h.svc.Create(&k)
	if err != nil {
		ErrorResponse(c, http.StatusInternalServerError, false, "创建失败")
		return
	}
	if h.reload != nil {
		_ = h.reload()
	}
	SuccessResponseWithMessage(c, "创建成功", gin.H{"id": id})
}

// Update 更新字段（含 has_quota），并自动热更新
func (h *OCRKeyHandler) Update(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		ErrorResponse(c, http.StatusBadRequest, false, "id错误")
		return
	}
	var req struct {
		Name           *string `json:"name"`
		IsActive       *bool   `json:"isActive"`
		HasQuota       *bool   `json:"hasQuota"`
		Weight         *int    `json:"weight"`
		MonthlyQuota   *int    `json:"monthlyQuota"`
		RemainingQuota *int    `json:"remainingQuota"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		ErrorResponse(c, http.StatusBadRequest, false, "参数错误")
		return
	}
	patch := map[string]interface{}{}
	if req.Name != nil {
		patch["name"] = *req.Name
	}
	if req.IsActive != nil {
		patch["is_active"] = *req.IsActive
	}
	if req.HasQuota != nil {
		patch["has_quota"] = *req.HasQuota
	}
	if req.Weight != nil && *req.Weight > 0 {
		patch["weight"] = *req.Weight
	}
	if req.MonthlyQuota != nil && *req.MonthlyQuota >= 0 {
		patch["monthly_quota"] = *req.MonthlyQuota
	}
	if req.RemainingQuota != nil && *req.RemainingQuota >= 0 {
		patch["remaining_quota"] = *req.RemainingQuota
	}
	if err := h.svc.Update(id, patch); err != nil {
		ErrorResponse(c, http.StatusInternalServerError, false, "更新失败")
		return
	}
	if h.reload != nil {
		_ = h.reload()
	}
	SuccessResponseWithMessage(c, "更新成功", nil)
}

// Delete 删除并自动热更新
func (h *OCRKeyHandler) Delete(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		ErrorResponse(c, http.StatusBadRequest, false, "id错误")
		return
	}
	if err := h.svc.Delete(id); err != nil {
		ErrorResponse(c, http.StatusInternalServerError, false, "删除失败")
		return
	}
	if h.reload != nil {
		_ = h.reload()
	}
	SuccessResponseWithMessage(c, "删除成功", nil)
}

func (h *OCRKeyHandler) RegisterRoutes(router *gin.RouterGroup, authMiddleware gin.HandlerFunc) {
	group := router.Group("/admin/ocr-keys", authMiddleware)
	{
		group.GET("", h.List)
		group.POST("", h.Create)
		group.PUT(":id", h.Update)
		group.DELETE(":id", h.Delete)
	}
}
