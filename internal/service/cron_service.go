package service

import (
	"time"
	"wjdr-backend-go/internal/client"
	"wjdr-backend-go/internal/repository"
	"wjdr-backend-go/internal/worker"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
)

// CronService 定时任务服务（与Node版本对齐）
type CronService struct {
	cron          *cron.Cron
	redeemRepo    *repository.RedeemRepository
	logRepo       *repository.LogRepository
	accountRepo   *repository.AccountRepository
	accountSvc    *AccountService
	ocrKeySvc     *OCRKeyService
	automationSvc *client.AutomationService
	workerManager *worker.Manager
	logger        *zap.Logger
	reloadOCRKeys func() error
}

func NewCronService(
	redeemRepo *repository.RedeemRepository,
	logRepo *repository.LogRepository,
	accountRepo *repository.AccountRepository,
	accountSvc *AccountService,
	ocrKeySvc *OCRKeyService,
	automationSvc *client.AutomationService,
	workerManager *worker.Manager,
	logger *zap.Logger,
	reloadOCRKeys func() error,
) *CronService {
	// 创建cron实例，使用秒级精度
	c := cron.New(cron.WithSeconds())

	return &CronService{
		cron:          c,
		redeemRepo:    redeemRepo,
		logRepo:       logRepo,
		accountSvc:    accountSvc,
		automationSvc: automationSvc,
		accountRepo:   accountRepo,
		workerManager: workerManager,
		logger:        logger,
		ocrKeySvc:     ocrKeySvc,
		reloadOCRKeys: reloadOCRKeys,
	}
}

// Start 启动定时任务（与Node版本对齐）
func (s *CronService) Start() error {
	s.logger.Info("🕒 启动定时任务服务")

	// 1. 自动清理过期兑换码 - 每天凌晨00:00执行（与Node版本一致）
	_, err := s.cron.AddFunc("0 0 0 * * *", s.cleanExpiredRedeemCodes)
	if err != nil {
		s.logger.Error("添加清理过期兑换码任务失败", zap.Error(err))
		return err
	}

	// 2. 自动补充兑换 - 每天凌晨00:10执行（与Node版本一致）
	_, err = s.cron.AddFunc("0 10 0 * * *", s.supplementRedeemCodes)
	if err != nil {
		s.logger.Error("添加补充兑换任务失败", zap.Error(err))
		return err
	}

	// 3. 每月1日00:00 重置OCR Key额度
	_, err = s.cron.AddFunc("0 0 0 1 * *", s.resetOCRMonthlyQuota)
	if err != nil {
		s.logger.Error("添加重置OCR额度任务失败", zap.Error(err))
		return err
	}

	// 4. 每天03:00 刷新所有用户数据
	_, err = s.cron.AddFunc("0 0 3 * * *", s.RefreshAllAccounts)
	if err != nil {
		s.logger.Error("添加刷新用户数据任务失败", zap.Error(err))
		return err
	}

	// 启动cron
	s.cron.Start()

	s.logger.Info("✅ 定时任务服务启动成功")
	s.logger.Info("📅 定时任务计划:")
	s.logger.Info("  - 00:00 清理过期兑换码")
	s.logger.Info("  - 00:10 自动补充兑换")
	s.logger.Info("  - 00:00(每月1日) 重置OCR Key额度")
	s.logger.Info("  - 03:00 刷新所有用户数据")

	return nil
}

// resetOCRMonthlyQuota 每月1号将剩余额度重置为每月额度，并热更新到内存
func (s *CronService) resetOCRMonthlyQuota() {
	s.logger.Info("🔁 开始执行OCR Key额度月度重置")
	if s.ocrKeySvc == nil {
		s.logger.Warn("OCRKeyService 未注入，跳过额度重置")
		return
	}
	if err := s.ocrKeySvc.ResetMonthlyQuota(); err != nil {
		s.logger.Error("重置OCR Key额度失败", zap.Error(err))
		return
	}
	if s.reloadOCRKeys != nil {
		if err := s.reloadOCRKeys(); err != nil {
			s.logger.Warn("重置后热更新OCR Keys失败", zap.Error(err))
		}
	}
	s.logger.Info("✅ OCR Key额度月度重置完成")
}

// refreshAllAccounts 每天03:00刷新所有活跃账号的数据（登录一次以更新昵称、头像、等级等）
// RefreshAllAccounts 导出：供管理端手动触发
func (s *CronService) RefreshAllAccounts() {
	s.logger.Info("🔄 开始刷新所有活跃账号数据")
	if s.accountSvc == nil {
		s.logger.Warn("AccountService 未注入，跳过刷新")
		return
	}
	accounts, err := s.accountRepo.GetActive()
	if err != nil {
		s.logger.Error("获取活跃账号失败", zap.Error(err))
		return
	}
	if len(accounts) == 0 {
		s.logger.Info("💫 无活跃账号需要刷新")
		return
	}
	updated := 0
	batch := 0
	for i, acc := range accounts {
		// 复用创建账号时的登录解析逻辑：调用 GameClient.Login 并写入账号表
		// 这里调用 AccountService.VerifyAccount 可更新 is_verified 和 last_login_check
		if _, err := s.accountSvc.VerifyAccount(acc.ID); err != nil {
			s.logger.Debug("刷新账号失败(验证)", zap.Int("id", acc.ID), zap.String("fid", acc.FID), zap.Error(err))
			continue
		}
		updated++
		batch++
		// 每批最多5个，批间隔3秒
		if batch%5 == 0 && i < len(accounts)-1 {
			s.logger.Info("⏸️ 批次间隔3秒(账号刷新)")
			select {
			case <-time.After(3 * time.Second):
			}
		}
	}
	s.logger.Info("✅ 刷新活跃账号数据完成", zap.Int("updated", updated), zap.Int("total", len(accounts)))
}

// Stop 停止定时任务
func (s *CronService) Stop() {
	s.logger.Info("🛑 停止定时任务服务")
	s.cron.Stop()
	s.logger.Info("✅ 定时任务服务已停止")
}

// cleanExpiredRedeemCodes 清理过期兑换码（与Node版本对齐）
func (s *CronService) cleanExpiredRedeemCodes() {
	s.logger.Info("🧹 开始执行清理过期兑换码任务")

	// 获取所有非长期兑换码
	codes, err := s.redeemRepo.GetNonLongTermCodes()
	if err != nil {
		s.logger.Error("获取非长期兑换码失败", zap.Error(err))
		return
	}

	if len(codes) == 0 {
		s.logger.Info("💫 没有需要检查的非长期兑换码")
		return
	}

	s.logger.Info("🔍 开始检查兑换码有效性", zap.Int("count", len(codes)))

	expiredCodes := []int{}
	testFID := "362872592" // 使用固定的测试FID（与Node版本一致）

	for _, code := range codes {
		s.logger.Info("🔍 检查兑换码",
			zap.Int("id", code.ID),
			zap.String("code", code.Code))

		// 使用备用账号测试兑换码
		result, err := s.automationSvc.RedeemSingle(testFID, code.Code)
		if err != nil {
			s.logger.Error("测试兑换码失败",
				zap.Error(err),
				zap.String("code", code.Code))
			continue
		}

		// 检查是否为过期或不存在的错误码（与Node逻辑一致）
		if result.ErrCode == 40007 { // 兑换码已过期
			s.logger.Info("⏰ 发现过期兑换码",
				zap.String("code", code.Code),
				zap.String("error", result.Error))
			expiredCodes = append(expiredCodes, code.ID)
		} else if result.ErrCode == 40014 { // 兑换码不存在
			s.logger.Info("❓ 发现不存在的兑换码",
				zap.String("code", code.Code),
				zap.String("error", result.Error))
			expiredCodes = append(expiredCodes, code.ID)
		} else {
			s.logger.Info("✅ 兑换码仍然有效",
				zap.String("code", code.Code))
		}
	}

	// 批量删除过期的兑换码
	if len(expiredCodes) > 0 {
		s.logger.Info("🗑️ 删除过期兑换码", zap.Int("count", len(expiredCodes)))

		deletedCount, err := s.redeemRepo.BulkDeleteRedeemCodes(expiredCodes)
		if err != nil {
			s.logger.Error("批量删除过期兑换码失败", zap.Error(err))
			return
		}

		s.logger.Info("✅ 清理过期兑换码完成",
			zap.Int("deleted_count", deletedCount),
			zap.Int("checked_count", len(codes)))
	} else {
		s.logger.Info("💫 没有发现过期的兑换码", zap.Int("checked_count", len(codes)))
	}
}

// supplementRedeemCodes 自动补充兑换（与Node版本对齐）
func (s *CronService) supplementRedeemCodes() {
	s.logger.Info("🔄 开始执行自动补充兑换任务")

	// 获取所有已完成的兑换码
	completedCodes, err := s.redeemRepo.GetCompletedRedeemCodes()
	if err != nil {
		s.logger.Error("获取已完成兑换码失败", zap.Error(err))
		return
	}

	if len(completedCodes) == 0 {
		s.logger.Info("💫 没有已完成的兑换码需要补充")
		return
	}

	s.logger.Info("🔍 开始检查补充兑换", zap.Int("codes_count", len(completedCodes)))

	supplementCount := 0

	for _, code := range completedCodes {
		s.logger.Info("🔍 检查兑换码补充需求",
			zap.Int("id", code.ID),
			zap.String("code", code.Code))

		// 获取已参与该兑换码的账号ID列表
		participatedAccountIDs, err := s.logRepo.GetParticipatedAccountIDs(code.ID)
		if err != nil {
			s.logger.Error("获取已参与账号列表失败",
				zap.Error(err),
				zap.String("code", code.Code))
			continue
		}

		// 若所有活跃已验证账号均已参与，则跳过（避免重复补充）
		activeAccounts, err := s.accountRepo.GetActive()
		if err != nil {
			s.logger.Error("获取活跃账号失败", zap.Error(err))
			continue
		}
		if len(activeAccounts) == 0 {
			s.logger.Info("💫 无活跃账号，跳过补充", zap.String("code", code.Code))
			continue
		}
		participated := make(map[int]bool, len(participatedAccountIDs))
		for _, id := range participatedAccountIDs {
			participated[id] = true
		}
		allDone := true
		for _, acc := range activeAccounts {
			if acc.IsVerified && !participated[acc.ID] {
				allDone = false
				break
			}
		}
		if allDone {
			s.logger.Info("✅ 该兑换码对当前账号集无需补充，跳过", zap.String("code", code.Code))
			continue
		}

		// 提交补充兑换任务
		jobID, err := s.workerManager.SubmitSupplementTask(code.ID)
		if err != nil {
			s.logger.Error("提交补充兑换任务失败",
				zap.Error(err),
				zap.String("code", code.Code))
			continue
		}

		// 降噪：提交任务日志改为 Debug，避免刷屏
		s.logger.Debug("📋 补充兑换任务已提交",
			zap.Int64("job_id", jobID),
			zap.String("code", code.Code),
			zap.Int("participated_accounts", len(participatedAccountIDs)))

		supplementCount++
	}

	s.logger.Info("✅ 自动补充兑换任务完成",
		zap.Int("submitted_count", supplementCount),
		zap.Int("total_codes", len(completedCodes)))
}
