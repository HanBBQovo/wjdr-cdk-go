package service

import (
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
	automationSvc *client.AutomationService
	workerManager *worker.Manager
	logger        *zap.Logger
}

func NewCronService(
	redeemRepo *repository.RedeemRepository,
	logRepo *repository.LogRepository,
	automationSvc *client.AutomationService,
	workerManager *worker.Manager,
	logger *zap.Logger,
) *CronService {
	// 创建cron实例，使用秒级精度
	c := cron.New(cron.WithSeconds())

	return &CronService{
		cron:          c,
		redeemRepo:    redeemRepo,
		logRepo:       logRepo,
		automationSvc: automationSvc,
		workerManager: workerManager,
		logger:        logger,
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

	// 启动cron
	s.cron.Start()

	s.logger.Info("✅ 定时任务服务启动成功")
	s.logger.Info("📅 定时任务计划:")
	s.logger.Info("  - 00:00 清理过期兑换码")
	s.logger.Info("  - 00:10 自动补充兑换")

	return nil
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

		// 这里可以进一步检查是否有新账号，但为了简化，我们直接提交补充任务
		// 让WorkerPool中的逻辑来处理是否有新账号需要补充

		// 提交补充兑换任务
		jobID, err := s.workerManager.SubmitSupplementTask(code.ID)
		if err != nil {
			s.logger.Error("提交补充兑换任务失败",
				zap.Error(err),
				zap.String("code", code.Code))
			continue
		}

		s.logger.Info("📋 补充兑换任务已提交",
			zap.Int64("job_id", jobID),
			zap.String("code", code.Code),
			zap.Int("participated_accounts", len(participatedAccountIDs)))

		supplementCount++
	}

	s.logger.Info("✅ 自动补充兑换任务完成",
		zap.Int("submitted_count", supplementCount),
		zap.Int("total_codes", len(completedCodes)))
}

