package service

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"wjdr-backend-go/internal/model"
	"wjdr-backend-go/internal/repository"

	"go.uber.org/zap"
)

// AdminService 管理员服务（与Node版本对齐）
type AdminService struct {
	adminRepo      *repository.AdminRepository
	AccountService *AccountService
	logger         *zap.Logger
}

// TokenExpireTime Token过期时间（30天，与Node版本一致）
const TokenExpireTime = 30 * 24 * time.Hour

func NewAdminService(
	adminRepo *repository.AdminRepository,
	accountSvc *AccountService,
	logger *zap.Logger,
) *AdminService {
	return &AdminService{
		adminRepo:      adminRepo,
		AccountService: accountSvc,
		logger:         logger,
	}
}

// VerifyPassword 验证管理员密码（与Node版本对齐）
func (s *AdminService) VerifyPassword(password string) (*model.APIResponse, error) {
	if password == "" {
		return &model.APIResponse{
			Success: false,
			Error:   "密码不能为空",
		}, nil
	}

	s.logger.Info("🔐 验证管理员密码")

	// 验证密码
	isValid, err := s.adminRepo.ValidatePassword(password)
	if err != nil {
		s.logger.Error("密码验证失败", zap.Error(err))
		return &model.APIResponse{
			Success: false,
			Error:   "密码验证失败",
		}, err
	}

	if !isValid {
		s.logger.Warn("❌ 管理员密码错误")
		return &model.APIResponse{
			Success: false,
			Error:   "密码错误",
		}, nil
	}

	// 生成新token
	token, err := s.generateToken()
	if err != nil {
		s.logger.Error("生成token失败", zap.Error(err))
		return &model.APIResponse{
			Success: false,
			Error:   "生成访问令牌失败",
		}, err
	}

	// 计算过期时间
	expiresAt := time.Now().Add(TokenExpireTime)

	// 添加token到数据库
	_, err = s.adminRepo.CreateToken(token, expiresAt)
	if err != nil {
		s.logger.Error("保存token失败", zap.Error(err))
		return &model.APIResponse{
			Success: false,
			Error:   "保存访问令牌失败",
		}, err
	}

	// 清理过期token
	go func() {
		cleanedCount, err := s.adminRepo.CleanExpiredTokens()
		if err != nil {
			s.logger.Error("清理过期token失败", zap.Error(err))
		} else if cleanedCount > 0 {
			s.logger.Info("清理过期token", zap.Int("count", cleanedCount))
		}
	}()

	s.logger.Info("✅ 管理员验证成功，token生成完成")

	return &model.APIResponse{
		Success: true,
		Message: "验证成功",
		Data: map[string]interface{}{
			"token":     token,
			"expiresIn": int64(TokenExpireTime.Milliseconds()),
		},
	}, nil
}

// VerifyToken 验证Token有效性（与Node版本对齐）
func (s *AdminService) VerifyToken(token string) (*model.APIResponse, error) {
	if token == "" {
		return &model.APIResponse{
			Success: false,
			Error:   "Token不能为空",
		}, nil
	}

	isValid, err := s.adminRepo.VerifyToken(token)
	if err != nil {
		s.logger.Error("Token验证失败", zap.Error(err))
		return &model.APIResponse{
			Success: false,
			Error:   "Token验证失败",
		}, err
	}

	if !isValid {
		return &model.APIResponse{
			Success: false,
			Error:   "Token无效或已过期",
		}, nil
	}

	return &model.APIResponse{
		Success: true,
		Message: "Token有效",
	}, nil
}

// GetAllPasswords 获取所有管理员密码信息（与Node版本对齐）
func (s *AdminService) GetAllPasswords() (*model.APIResponse, error) {
	passwords, err := s.adminRepo.GetAllPasswords()
	if err != nil {
		s.logger.Error("获取密码列表失败", zap.Error(err))
		return &model.APIResponse{
			Success: false,
			Error:   "获取密码列表失败",
		}, err
	}

	return &model.APIResponse{
		Success: true,
		Data:    passwords,
	}, nil
}

// CreatePassword 创建新的管理员密码（与Node版本对齐）
func (s *AdminService) CreatePassword(password, description string) (*model.APIResponse, error) {
	if password == "" {
		return &model.APIResponse{
			Success: false,
			Error:   "密码不能为空",
		}, nil
	}

	if description == "" {
		description = "新建密码"
	}

	s.logger.Info("🔑 创建新管理员密码", zap.String("description", description))

	id, err := s.adminRepo.CreatePassword(password, description)
	if err != nil {
		s.logger.Error("创建密码失败", zap.Error(err))
		return &model.APIResponse{
			Success: false,
			Error:   "创建密码失败",
		}, err
	}

	s.logger.Info("✅ 管理员密码创建成功", zap.Int("id", id))

	return &model.APIResponse{
		Success: true,
		Message: "密码创建成功",
		Data: map[string]interface{}{
			"id": id,
		},
	}, nil
}

// DeletePassword 删除管理员密码（与Node版本对齐）
func (s *AdminService) DeletePassword(id int) (*model.APIResponse, error) {
	s.logger.Info("🗑️ 删除管理员密码", zap.Int("id", id))

	success, err := s.adminRepo.DeletePassword(id)
	if err != nil {
		s.logger.Error("删除密码失败", zap.Error(err))
		return &model.APIResponse{
			Success: false,
			Error:   err.Error(),
		}, err
	}

	if !success {
		return &model.APIResponse{
			Success: false,
			Error:   "密码不存在或无法删除",
		}, nil
	}

	s.logger.Info("✅ 管理员密码删除成功", zap.Int("id", id))

	return &model.APIResponse{
		Success: true,
		Message: "密码删除成功",
	}, nil
}

// UpdatePasswordStatus 更新密码状态（与Node版本对齐）
func (s *AdminService) UpdatePasswordStatus(id int, isActive bool) (*model.APIResponse, error) {
	status := "禁用"
	if isActive {
		status = "启用"
	}

	s.logger.Info("🔄 更新密码状态",
		zap.Int("id", id),
		zap.String("status", status))

	success, err := s.adminRepo.UpdatePasswordStatus(id, isActive)
	if err != nil {
		s.logger.Error("更新密码状态失败", zap.Error(err))
		return &model.APIResponse{
			Success: false,
			Error:   "更新密码状态失败",
		}, err
	}

	if !success {
		return &model.APIResponse{
			Success: false,
			Error:   "密码不存在",
		}, nil
	}

	s.logger.Info("✅ 密码状态更新成功",
		zap.Int("id", id),
		zap.String("status", status))

	return &model.APIResponse{
		Success: true,
		Message: "密码状态更新成功",
	}, nil
}

// UpdateDefaultPassword 更新默认密码（与Node版本对齐）
func (s *AdminService) UpdateDefaultPassword(newPassword, description string) (*model.APIResponse, error) {
	if newPassword == "" {
		return &model.APIResponse{
			Success: false,
			Error:   "新密码不能为空",
		}, nil
	}

	if description == "" {
		description = "默认管理员密码"
	}

	s.logger.Info("🔑 更新默认管理员密码")

	success, err := s.adminRepo.UpdateDefaultPassword(1, newPassword, description)
	if err != nil {
		s.logger.Error("更新默认密码失败", zap.Error(err))
		return &model.APIResponse{
			Success: false,
			Error:   err.Error(),
		}, err
	}

	if !success {
		return &model.APIResponse{
			Success: false,
			Error:   "更新失败",
		}, nil
	}

	s.logger.Info("✅ 默认管理员密码更新成功")

	return &model.APIResponse{
		Success: true,
		Message: "默认密码更新成功",
	}, nil
}

// generateToken 生成随机token（与Node版本对齐）
func (s *AdminService) generateToken() (string, error) {
	// 生成32字节的随机数据
	bytes := make([]byte, 32)
	_, err := rand.Read(bytes)
	if err != nil {
		return "", err
	}

	// 转换为十六进制字符串
	return hex.EncodeToString(bytes), nil
}

// RevokeToken 撤销Token（额外功能）
func (s *AdminService) RevokeToken(token string) (*model.APIResponse, error) {
	if token == "" {
		return &model.APIResponse{
			Success: false,
			Error:   "Token不能为空",
		}, nil
	}

	success, err := s.adminRepo.DeleteToken(token)
	if err != nil {
		s.logger.Error("撤销token失败", zap.Error(err))
		return &model.APIResponse{
			Success: false,
			Error:   "撤销token失败",
		}, err
	}

	if !success {
		return &model.APIResponse{
			Success: false,
			Error:   "Token不存在",
		}, nil
	}

	s.logger.Info("✅ Token撤销成功")

	return &model.APIResponse{
		Success: true,
		Message: "Token撤销成功",
	}, nil
}
