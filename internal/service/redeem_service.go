package service

import (
	"fmt"

	"wjdr-backend-go/internal/client"
	"wjdr-backend-go/internal/model"
	"wjdr-backend-go/internal/repository"
	"wjdr-backend-go/internal/worker"

	"go.uber.org/zap"
)

// RedeemService 兑换服务（与Node版本对齐）
type RedeemService struct {
	redeemRepo    *repository.RedeemRepository
	accountRepo   *repository.AccountRepository
	logRepo       *repository.LogRepository
	automationSvc *client.AutomationService
	workerManager *worker.Manager
	logger        *zap.Logger
}

func NewRedeemService(
	redeemRepo *repository.RedeemRepository,
	accountRepo *repository.AccountRepository,
	logRepo *repository.LogRepository,
	automationSvc *client.AutomationService,
	workerManager *worker.Manager,
	logger *zap.Logger,
) *RedeemService {
	return &RedeemService{
		redeemRepo:    redeemRepo,
		accountRepo:   accountRepo,
		logRepo:       logRepo,
		automationSvc: automationSvc,
		workerManager: workerManager,
		logger:        logger,
	}
}

// SubmitRedeemCode 提交新的兑换码（与Node版本对齐）
func (s *RedeemService) SubmitRedeemCode(code string, isLong bool) (*model.APIResponse, error) {
	if code == "" {
		return &model.APIResponse{
			Success: false,
			Error:   "兑换码不能为空",
		}, nil
	}

	s.logger.Info("📝 提交新兑换码",
		zap.String("code", code),
		zap.Bool("is_long", isLong))

	// 检查兑换码是否已存在
	existingCode, err := s.redeemRepo.FindRedeemCodeByCode(code)
	if err != nil {
		s.logger.Error("查询兑换码失败", zap.Error(err))
		return &model.APIResponse{
			Success: false,
			Error:   "查询兑换码失败",
		}, err
	}

	if existingCode != nil {
		return &model.APIResponse{
			Success: false,
			Error:   "兑换码已存在",
			Data:    existingCode,
		}, nil
	}

	// 获取活跃账号列表用于预验证
	accounts, err := s.accountRepo.GetAll()
	if err != nil {
		s.logger.Error("获取账号列表失败", zap.Error(err))
		return &model.APIResponse{
			Success: false,
			Error:   "获取账号列表失败",
		}, err
	}

	// 筛选活跃且已验证的账号
	var activeAccounts []model.Account
	for _, acc := range accounts {
		if acc.IsActive && acc.IsVerified {
			activeAccounts = append(activeAccounts, acc)
		}
	}

	// 选择测试账号进行预验证（与Node逻辑一致）
	var testFID string
	if len(activeAccounts) > 0 {
		// 有活跃账号时使用第一个活跃账号
		testFID = activeAccounts[0].FID
		s.logger.Info("📋 使用活跃账号进行预验证",
			zap.String("test_fid", testFID))
	} else {
		// 没有活跃账号时使用备用FID（与Node逻辑一致）
		testFID = "362872592"
		s.logger.Info("📋 使用备用账号进行预验证",
			zap.String("test_fid", testFID))
	}

	// 预验证兑换码（与Node逻辑一致）
	s.logger.Info("🔍 开始预验证兑换码", zap.String("code", code))

	verifyResult, err := s.automationSvc.RedeemSingle(testFID, code)
	if err != nil {
		s.logger.Error("预验证异常", zap.Error(err))
		return &model.APIResponse{
			Success: false,
			Error:   "预验证时发生异常",
		}, err
	}

	// 检查预验证结果
	if verifyResult.IsFatal {
		s.logger.Warn("❌ 预验证发现致命错误",
			zap.String("error", verifyResult.Error),
			zap.Int("err_code", verifyResult.ErrCode))

		return &model.APIResponse{
			Success: false,
			Error:   fmt.Sprintf("兑换码验证失败: %s", verifyResult.Error),
		}, nil
	}

	// 预验证通过，创建兑换码记录
	redeemCodeID, err := s.redeemRepo.CreateRedeemCode(code, isLong)
	if err != nil {
		s.logger.Error("创建兑换码失败", zap.Error(err))
		return &model.APIResponse{
			Success: false,
			Error:   "创建兑换码失败",
		}, err
	}

	// 获取创建的兑换码信息
	redeemCode, err := s.redeemRepo.FindRedeemCodeByID(redeemCodeID)
	if err != nil {
		s.logger.Error("获取兑换码信息失败", zap.Error(err))
		return &model.APIResponse{
			Success: false,
			Error:   "获取兑换码信息失败",
		}, err
	}

	s.logger.Info("✅ 预验证通过，兑换码已创建",
		zap.Int("redeem_code_id", redeemCodeID),
		zap.String("code", code))

	// 异步提交批量兑换任务
	jobID, err := s.workerManager.SubmitRedeemTask(redeemCodeID, nil) // nil表示处理所有活跃账号
	if err != nil {
		s.logger.Error("提交兑换任务失败", zap.Error(err))
		// 这里不返回错误，因为兑换码已经创建，只是异步处理失败
		s.logger.Warn("⚠️ 兑换码已创建但异步任务提交失败，请手动重试")
	} else {
		s.logger.Info("📋 兑换任务已提交",
			zap.Int64("job_id", jobID),
			zap.Int("redeem_code_id", redeemCodeID))
	}

	return &model.APIResponse{
		Success: true,
		Message: "兑换码验证通过，正在后台处理...",
		Data:    redeemCode,
	}, nil
}

// GetAllRedeemCodes 获取全部兑换码（去除分页）
func (s *RedeemService) GetAllRedeemCodes() (*model.APIResponse, error) {
	codes, err := s.redeemRepo.GetAllRedeemCodesAll()
	if err != nil {
		s.logger.Error("获取兑换码列表失败", zap.Error(err))
		return &model.APIResponse{Success: false, Error: "获取兑换码列表失败"}, err
	}
	return &model.APIResponse{Success: true, Data: codes}, nil
}

// GetRedeemCodeDetails 获取兑换码详细信息（与Node版本对齐）
func (s *RedeemService) GetRedeemCodeDetails(id int) (*model.APIResponse, error) {
	redeemCode, err := s.redeemRepo.FindRedeemCodeByID(id)
	if err != nil {
		s.logger.Error("获取兑换码失败", zap.Error(err))
		return &model.APIResponse{
			Success: false,
			Error:   "获取兑换码失败",
		}, err
	}

	if redeemCode == nil {
		return &model.APIResponse{
			Success: false,
			Error:   "兑换码不存在",
		}, nil
	}

	return &model.APIResponse{
		Success: true,
		Data:    redeemCode,
	}, nil
}

// GetRedeemCodeLogs 获取兑换码的兑换日志（与Node版本对齐）
func (s *RedeemService) GetRedeemCodeLogs(id int) (*model.APIResponse, error) {
	logs, err := s.logRepo.GetLogsByRedeemCodeID(id)
	if err != nil {
		s.logger.Error("获取兑换日志失败", zap.Error(err))
		return &model.APIResponse{
			Success: false,
			Error:   "获取兑换日志失败",
		}, err
	}

	return &model.APIResponse{
		Success: true,
		Data:    logs,
	}, nil
}

// GetAllLogs 获取所有兑换日志（去除分页/限制）
func (s *RedeemService) GetAllLogs() (*model.APIResponse, error) {
	logs, err := s.logRepo.GetAllLogs()
	if err != nil {
		s.logger.Error("获取兑换日志失败", zap.Error(err))
		return &model.APIResponse{Success: false, Error: "获取兑换日志失败"}, err
	}
	return &model.APIResponse{Success: true, Data: logs, Message: "ok"}, nil
}

// GetAllLogsFiltered 获取全部日志，并支持通过 result=success|failed 过滤（去除分页/限制）
func (s *RedeemService) GetAllLogsFiltered(result string) (*model.APIResponse, error) {
	if result != "success" && result != "failed" && result != "" {
		return &model.APIResponse{Success: false, Error: "result 参数仅支持 success/failed 或留空"}, nil
	}

	logs, err := s.logRepo.GetAllLogsFiltered(result)
	if err != nil {
		s.logger.Error("获取兑换日志失败", zap.Error(err))
		return &model.APIResponse{Success: false, Error: "获取兑换日志失败"}, err
	}
	return &model.APIResponse{Success: true, Data: logs}, nil
}

// GetGlobalLogStats 获取全局日志统计（不受过滤影响）
func (s *RedeemService) GetGlobalLogStats() (total, success, failed int, err error) {
	return s.logRepo.GetGlobalLogStats()
}

// DeleteRedeemCode 删除兑换码（与Node版本对齐）
func (s *RedeemService) DeleteRedeemCode(id int) (*model.APIResponse, error) {
	// 检查兑换码是否存在
	redeemCode, err := s.redeemRepo.FindRedeemCodeByID(id)
	if err != nil {
		s.logger.Error("查询兑换码失败", zap.Error(err))
		return &model.APIResponse{
			Success: false,
			Error:   "查询兑换码失败",
		}, err
	}

	if redeemCode == nil {
		return &model.APIResponse{
			Success: false,
			Error:   "兑换码不存在",
		}, nil
	}

	s.logger.Info("🗑️ 删除兑换码",
		zap.Int("id", id),
		zap.String("code", redeemCode.Code))

	err = s.redeemRepo.DeleteRedeemCode(id)
	if err != nil {
		s.logger.Error("删除兑换码失败", zap.Error(err))
		return &model.APIResponse{
			Success: false,
			Error:   "删除兑换码失败",
		}, err
	}

	s.logger.Info("✅ 兑换码删除成功",
		zap.Int("id", id),
		zap.String("code", redeemCode.Code))

	return &model.APIResponse{
		Success: true,
		Message: "兑换码删除成功",
	}, nil
}

// BulkDeleteRedeemCodes 批量删除兑换码（与Node版本对齐）
func (s *RedeemService) BulkDeleteRedeemCodes(ids []int) (*model.APIResponse, error) {
	if len(ids) == 0 {
		return &model.APIResponse{
			Success: false,
			Error:   "没有指定要删除的兑换码",
		}, nil
	}

	s.logger.Info("🗑️ 批量删除兑换码", zap.Int("count", len(ids)))

	deletedCount, err := s.redeemRepo.BulkDeleteRedeemCodes(ids)
	if err != nil {
		s.logger.Error("批量删除兑换码失败", zap.Error(err))
		return &model.APIResponse{
			Success: false,
			Error:   "批量删除兑换码失败",
		}, err
	}

	s.logger.Info("✅ 批量删除兑换码成功", zap.Int("deleted_count", deletedCount))

	return &model.APIResponse{
		Success: true,
		Message: fmt.Sprintf("成功删除 %d 个兑换码", deletedCount),
		Data: map[string]interface{}{
			"deleted_count": deletedCount,
		},
	}, nil
}

// RetryRedeemCode 重试兑换码（与Node版本对齐）
func (s *RedeemService) RetryRedeemCode(id int) (*model.APIResponse, error) {
	// 检查兑换码是否存在
	redeemCode, err := s.redeemRepo.FindRedeemCodeByID(id)
	if err != nil {
		s.logger.Error("查询兑换码失败", zap.Error(err))
		return &model.APIResponse{
			Success: false,
			Error:   "查询兑换码失败",
		}, err
	}

	if redeemCode == nil {
		return &model.APIResponse{
			Success: false,
			Error:   "兑换码不存在",
		}, nil
	}

	if redeemCode.Status != "completed" {
		return &model.APIResponse{
			Success: false,
			Error:   "只能重试已完成的兑换码",
		}, nil
	}

	s.logger.Info("🔄 重试兑换码",
		zap.Int("id", id),
		zap.String("code", redeemCode.Code))

	// 提交补充兑换任务（为新账号执行兑换）
	jobID, err := s.workerManager.SubmitSupplementTask(id)
	if err != nil {
		s.logger.Error("提交补充兑换任务失败", zap.Error(err))
		return &model.APIResponse{
			Success: false,
			Error:   "提交补充兑换任务失败",
		}, err
	}

	s.logger.Info("📋 补充兑换任务已提交",
		zap.Int64("job_id", jobID),
		zap.Int("redeem_code_id", id))

	return &model.APIResponse{
		Success: true,
		Message: "补充兑换任务已提交，正在后台处理",
		Data: map[string]interface{}{
			"job_id": jobID,
		},
	}, nil
}

// RetryRedeemCodes 批量重试多个兑换码（在现有补充兑换机制上逐个提交后台任务）
func (s *RedeemService) RetryRedeemCodes(ids []int) (*model.APIResponse, error) {
	if len(ids) == 0 {
		return &model.APIResponse{Success: false, Error: "没有指定要补充兑换的兑换码"}, nil
	}

	submitted := 0
	failed := 0
	jobIDs := make([]int64, 0, len(ids))
	invalidIDs := make([]int, 0)

	for _, id := range ids {
		// 校验兑换码
		redeemCode, err := s.redeemRepo.FindRedeemCodeByID(id)
		if err != nil || redeemCode == nil {
			s.logger.Warn("跳过不存在的兑换码", zap.Int("redeem_code_id", id))
			failed++
			invalidIDs = append(invalidIDs, id)
			continue
		}
		if redeemCode.Status != "completed" {
			s.logger.Warn("兑换码状态非completed，跳过", zap.Int("redeem_code_id", id), zap.String("status", redeemCode.Status))
			failed++
			continue
		}

		jobID, err := s.workerManager.SubmitSupplementTask(id)
		if err != nil {
			s.logger.Error("提交补充兑换任务失败", zap.Int("redeem_code_id", id), zap.Error(err))
			failed++
			continue
		}
		submitted++
		jobIDs = append(jobIDs, jobID)
	}

	return &model.APIResponse{
		Success: true,
		Message: fmt.Sprintf("已提交 %d 个补充兑换任务，跳过/失败 %d 个", submitted, failed),
		Data: map[string]interface{}{
			"submitted": submitted,
			"failed":    failed,
			"job_ids":   jobIDs,
			"invalid":   invalidIDs,
		},
	}, nil
}

// GetAccountsForRedeemCode 获取兑换码的账号处理状态（与Node版本对齐）
func (s *RedeemService) GetAccountsForRedeemCode(id int) (*model.APIResponse, error) {
	// 检查兑换码是否存在
	redeemCode, err := s.redeemRepo.FindRedeemCodeByID(id)
	if err != nil {
		s.logger.Error("查询兑换码失败", zap.Error(err))
		return &model.APIResponse{
			Success: false,
			Error:   "查询兑换码失败",
		}, err
	}

	if redeemCode == nil {
		return &model.APIResponse{
			Success: false,
			Error:   "兑换码不存在",
		}, nil
	}

	// 获取所有账号
	accounts, err := s.accountRepo.GetAll()
	if err != nil {
		s.logger.Error("获取账号列表失败", zap.Error(err))
		return &model.APIResponse{
			Success: false,
			Error:   "获取账号列表失败",
		}, err
	}

	// 获取该兑换码的所有日志
	logs, err := s.logRepo.GetLogsByRedeemCodeID(id)
	if err != nil {
		s.logger.Error("获取兑换日志失败", zap.Error(err))
		return &model.APIResponse{
			Success: false,
			Error:   "获取兑换日志失败",
		}, err
	}

	// 创建账号状态映射
	accountStatusMap := make(map[int]map[string]interface{})
	for _, log := range logs {
		accountStatusMap[log.GameAccountID] = map[string]interface{}{
			"result":             log.Result,
			"error_message":      log.ErrorMessage,
			"success_message":    log.SuccessMessage,
			"captcha_recognized": log.CaptchaRecognized,
			"processing_time":    log.ProcessingTime,
			"err_code":           log.ErrCode,
			"redeemed_at":        log.RedeemedAt,
		}
	}

	// 构建响应数据
	var accountResults []map[string]interface{}
	for _, account := range accounts {
		result := map[string]interface{}{
			"id":          account.ID,
			"fid":         account.FID,
			"nickname":    account.Nickname,
			"is_active":   account.IsActive,
			"is_verified": account.IsVerified,
		}

		if status, exists := accountStatusMap[account.ID]; exists {
			result["status"] = "processed"
			result["result"] = status
		} else {
			result["status"] = "not_processed"
			result["result"] = nil
		}

		accountResults = append(accountResults, result)
	}

	return &model.APIResponse{
		Success: true,
		Data:    accountResults,
	}, nil
}
