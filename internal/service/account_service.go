package service

import (
	"fmt"

	"wjdr-backend-go/internal/client"
	"wjdr-backend-go/internal/model"
	"wjdr-backend-go/internal/repository"

	"go.uber.org/zap"
)

// AccountService 账号服务（与Node版本对齐）
type AccountService struct {
	accountRepo *repository.AccountRepository
	gameClient  *client.GameClient
	logger      *zap.Logger
}

func NewAccountService(
	accountRepo *repository.AccountRepository,
	gameClient *client.GameClient,
	logger *zap.Logger,
) *AccountService {
	return &AccountService{
		accountRepo: accountRepo,
		gameClient:  gameClient,
		logger:      logger,
	}
}

// GetAllAccounts 获取所有账号（与Node版本对齐）
func (s *AccountService) GetAllAccounts() ([]model.Account, error) {
	return s.accountRepo.GetAll()
}

// CreateAccount 创建新账号（与Node版本对齐）
func (s *AccountService) CreateAccount(fid string) (*model.APIResponse, error) {
	if fid == "" {
		return &model.APIResponse{
			Success: false,
			Error:   "FID不能为空",
		}, nil
	}

	// 检查账号是否已存在
	existingAccount, err := s.accountRepo.FindByFID(fid)
	if err != nil {
		s.logger.Error("查询账号失败", zap.Error(err))
		return &model.APIResponse{
			Success: false,
			Error:   "查询账号失败",
		}, err
	}

	if existingAccount != nil {
		return &model.APIResponse{
			Success: false,
			Error:   "账号已存在",
			Data:    existingAccount,
		}, nil
	}

	// 验证账号有效性（与Node版本逻辑一致）
	s.logger.Info("🔍 验证账号", zap.String("fid", fid))

	loginResult, err := s.gameClient.Login(fid)
	if err != nil {
		s.logger.Error("账号验证异常", zap.Error(err))
		return &model.APIResponse{
			Success: false,
			Error:   "验证账号时发生异常",
		}, err
	}

	if !loginResult.Success {
		s.logger.Warn("账号验证失败",
			zap.String("fid", fid),
			zap.String("error", loginResult.Error))

		return &model.APIResponse{
			Success: false,
			Error:   fmt.Sprintf("账号验证失败: %s", loginResult.Error),
		}, nil
	}

	// 从验证结果中解析用户信息（与Node逻辑一致）
	userData := loginResult.Data.(map[string]interface{})

	nickname := ""
	if n, ok := userData["nickname"]; ok && n != nil {
		nickname = n.(string)
	}

	var avatarImage *string
	if a, ok := userData["avatar_image"]; ok && a != nil && a.(string) != "" {
		avatar := a.(string)
		avatarImage = &avatar
	}

	var stoveLv *int
	if s, ok := userData["stove_lv"]; ok && s != nil {
		if level, ok := s.(int); ok {
			stoveLv = &level
		} else if level, ok := s.(float64); ok {
			levelInt := int(level)
			stoveLv = &levelInt
		}
	}

	var stoveLvContent *string
	if c, ok := userData["stove_lv_content"]; ok && c != nil && c.(string) != "" {
		content := c.(string)
		stoveLvContent = &content
	}

	// 创建账号
	accountID, err := s.accountRepo.Create(fid, nickname, avatarImage, stoveLv, stoveLvContent)
	if err != nil {
		s.logger.Error("创建账号失败", zap.Error(err))
		return &model.APIResponse{
			Success: false,
			Error:   "创建账号失败",
		}, err
	}

	// 获取创建的账号信息
	account, err := s.accountRepo.FindByFID(fid)
	if err != nil {
		s.logger.Error("获取新创建的账号失败", zap.Error(err))
		return &model.APIResponse{
			Success: false,
			Error:   "获取账号信息失败",
		}, err
	}

	s.logger.Info("✅ 账号创建成功",
		zap.Int("id", accountID),
		zap.String("fid", fid),
		zap.String("nickname", nickname))

	return &model.APIResponse{
		Success: true,
		Message: "账号验证成功并已添加",
		Data:    account,
	}, nil
}

// VerifyAccount 手动验证账号（与Node版本对齐）
func (s *AccountService) VerifyAccount(id int) (*model.APIResponse, error) {
	// 先获取账号信息
	accounts, err := s.accountRepo.GetAll()
	if err != nil {
		return &model.APIResponse{
			Success: false,
			Error:   "获取账号信息失败",
		}, err
	}

	var targetAccount *model.Account
	for _, acc := range accounts {
		if acc.ID == id {
			targetAccount = &acc
			break
		}
	}

	if targetAccount == nil {
		return &model.APIResponse{
			Success: false,
			Error:   "账号不存在",
		}, nil
	}

	s.logger.Info("🔍 手动验证账号",
		zap.Int("id", id),
		zap.String("fid", targetAccount.FID))

	// 验证账号
	loginResult, err := s.gameClient.Login(targetAccount.FID)
	if err != nil {
		s.logger.Error("账号验证异常", zap.Error(err))
		return &model.APIResponse{
			Success: false,
			Error:   "验证账号时发生异常",
		}, err
	}

	// 更新验证状态
	err = s.accountRepo.UpdateVerifyStatus(id, loginResult.Success)
	if err != nil {
		s.logger.Error("更新验证状态失败", zap.Error(err))
		return &model.APIResponse{
			Success: false,
			Error:   "更新验证状态失败",
		}, err
	}

	if loginResult.Success {
		s.logger.Info("✅ 账号验证成功",
			zap.Int("id", id),
			zap.String("fid", targetAccount.FID))

		return &model.APIResponse{
			Success: true,
			Message: "账号验证成功",
		}, nil
	} else {
		s.logger.Warn("❌ 账号验证失败",
			zap.Int("id", id),
			zap.String("fid", targetAccount.FID),
			zap.String("error", loginResult.Error))

		return &model.APIResponse{
			Success: false,
			Error:   fmt.Sprintf("账号验证失败: %s", loginResult.Error),
		}, nil
	}
}

// DeleteAccount 删除账号（与Node版本对齐）
func (s *AccountService) DeleteAccount(id int) (*model.APIResponse, error) {
	// 检查账号是否存在
	accounts, err := s.accountRepo.GetAll()
	if err != nil {
		return &model.APIResponse{
			Success: false,
			Error:   "获取账号信息失败",
		}, err
	}

	var targetAccount *model.Account
	for _, acc := range accounts {
		if acc.ID == id {
			targetAccount = &acc
			break
		}
	}

	if targetAccount == nil {
		return &model.APIResponse{
			Success: false,
			Error:   "账号不存在",
		}, nil
	}

	s.logger.Info("🗑️ 删除账号",
		zap.Int("id", id),
		zap.String("fid", targetAccount.FID))

	// 删除账号（包含统计更新逻辑）
	err = s.accountRepo.Delete(id)
	if err != nil {
		s.logger.Error("删除账号失败", zap.Error(err))
		return &model.APIResponse{
			Success: false,
			Error:   "删除账号失败",
		}, err
	}

	// 为避免极端情况下统计未即时刷新，这里再做一次全量统计修复
	if _, fixErr := s.accountRepo.FixAllRedeemCodeStats(); fixErr != nil {
		// 不阻断删除流程，仅记录日志
		s.logger.Warn("删除账号后修复统计失败", zap.Error(fixErr))
	}

	s.logger.Info("✅ 账号删除成功",
		zap.Int("id", id),
		zap.String("fid", targetAccount.FID))

	return &model.APIResponse{
		Success: true,
		Message: "账号删除成功",
	}, nil
}

// BulkDeleteAccounts 批量删除账号
func (s *AccountService) BulkDeleteAccounts(ids []int) (*model.APIResponse, error) {
	if len(ids) == 0 {
		return &model.APIResponse{Success: false, Error: "请提供要删除的账号ID列表"}, nil
	}

	deletedCount, err := s.accountRepo.BulkDelete(ids)
	if err != nil {
		s.logger.Error("批量删除账号失败", zap.Error(err))
		return &model.APIResponse{Success: false, Error: "批量删除账号失败"}, err
	}

	// 删除后做一次统计修复（与单个删除保持一致的最终一致性策略）
	if _, fixErr := s.accountRepo.FixAllRedeemCodeStats(); fixErr != nil {
		s.logger.Warn("批量删除账号后修复统计失败", zap.Error(fixErr))
	}

	return &model.APIResponse{
		Success: true,
		Message: "批量删除账号成功",
		Data:    map[string]interface{}{"deletedCount": deletedCount},
	}, nil
}

// FixAllStats 修复所有兑换码统计（与Node版本对齐）
func (s *AccountService) FixAllStats() (*model.APIResponse, error) {
	s.logger.Info("🔧 开始修复所有兑换码统计")

	fixedCount, err := s.accountRepo.FixAllRedeemCodeStats()
	if err != nil {
		s.logger.Error("修复统计失败", zap.Error(err))
		return &model.APIResponse{
			Success: false,
			Error:   "修复统计失败",
		}, err
	}

	s.logger.Info("✅ 修复统计完成", zap.Int("fixed_count", fixedCount))

	return &model.APIResponse{
		Success: true,
		Message: fmt.Sprintf("已修复 %d 个兑换码的统计数据", fixedCount),
		Data: map[string]interface{}{
			"fixed_count": fixedCount,
		},
	}, nil
}
