package worker

import (
	"context"
	"fmt"
	"sync"
	"time"

	"wjdr-backend-go/internal/client"
	"wjdr-backend-go/internal/model"
	"wjdr-backend-go/internal/repository"

	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

// WorkerPool Worker池（可配置并发度，带限流控制）
type WorkerPool struct {
	concurrency   int
	jobQueue      *JobQueue
	automationSvc *client.AutomationService
	accountRepo   *repository.AccountRepository
	redeemRepo    *repository.RedeemRepository
	logRepo       *repository.LogRepository
	rateLimiter   *rate.Limiter
	logger        *zap.Logger
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	workerWg      sync.WaitGroup
}

// WorkerPoolConfig Worker池配置
type WorkerPoolConfig struct {
	Concurrency  int // Worker并发数
	RateLimitQPS int // 外部API限流 (请求/秒)
}

func NewWorkerPool(
	config WorkerPoolConfig,
	jobQueue *JobQueue,
	automationSvc *client.AutomationService,
	accountRepo *repository.AccountRepository,
	redeemRepo *repository.RedeemRepository,
	logRepo *repository.LogRepository,
	logger *zap.Logger,
) *WorkerPool {
	ctx, cancel := context.WithCancel(context.Background())

	// 创建限流器 (每秒允许的请求数，突发容量为并发数的2倍)
	limiter := rate.NewLimiter(rate.Limit(config.RateLimitQPS), config.Concurrency*2)

	return &WorkerPool{
		concurrency:   config.Concurrency,
		jobQueue:      jobQueue,
		automationSvc: automationSvc,
		accountRepo:   accountRepo,
		redeemRepo:    redeemRepo,
		logRepo:       logRepo,
		rateLimiter:   limiter,
		logger:        logger,
		ctx:           ctx,
		cancel:        cancel,
	}
}

// Start 启动Worker池
func (wp *WorkerPool) Start() {
	wp.logger.Info("🚀 Worker池启动",
		zap.Int("concurrency", wp.concurrency),
		zap.String("rate_limit", fmt.Sprintf("%d qps", int(wp.rateLimiter.Limit()))))

	// 启动多个Worker
	for i := 0; i < wp.concurrency; i++ {
		wp.workerWg.Add(1)
		go wp.worker(i)
	}

	// 停止定时监控日志输出（移除定时打印“📊 Worker池状态”）
}

// Stop 停止Worker池
func (wp *WorkerPool) Stop() {
	wp.logger.Info("🛑 停止Worker池...")
	wp.cancel()

	// 等待所有Worker完成
	wp.workerWg.Wait()

	wp.logger.Info("✅ Worker池已停止")
}

// worker 单个Worker的工作循环
func (wp *WorkerPool) worker(workerID int) {
	defer wp.workerWg.Done()

	wp.logger.Debug("👷 Worker启动", zap.Int("worker_id", workerID))

	for {
		select {
		case <-wp.ctx.Done():
			wp.logger.Debug("👷 Worker停止", zap.Int("worker_id", workerID))
			return
		case job := <-wp.jobQueue.Dequeue():
			if job == nil {
				continue
			}

			wp.processJob(workerID, job)
		}
	}
}

// processJob 处理单个任务
func (wp *WorkerPool) processJob(workerID int, job *Job) {
	startTime := time.Now()

	wp.logger.Debug("🔨 开始处理任务",
		zap.Int("worker_id", workerID),
		zap.Int64("job_id", job.ID),
		zap.String("type", job.Type))

	// 标记任务为处理中
	if err := wp.jobQueue.MarkJobProcessing(job.ID); err != nil {
		wp.logger.Error("标记任务处理中失败", zap.Error(err))
		return
	}

	// 限流控制
	if err := wp.rateLimiter.Wait(wp.ctx); err != nil {
		wp.logger.Error("限流等待被取消", zap.Error(err))
		return
	}

	var err error
	switch job.Type {
	case JobTypeRedeem:
		err = wp.processRedeemJob(job)
	case JobTypeRetryRedeem:
		err = wp.processRetryRedeemJob(job)
	case JobTypeSupplementRedeem:
		err = wp.processSupplementRedeemJob(job)
	default:
		err = fmt.Errorf("未知任务类型: %s", job.Type)
	}

	duration := time.Since(startTime)

	if err != nil {
		wp.logger.Error("❌ 任务处理失败",
			zap.Int("worker_id", workerID),
			zap.Int64("job_id", job.ID),
			zap.Error(err),
			zap.Duration("duration", duration))

		// 尝试重试
		if retryErr := wp.jobQueue.RetryJob(job, err.Error()); retryErr != nil {
			wp.logger.Error("任务重试失败", zap.Error(retryErr))
		}
	} else {
		wp.logger.Debug("✅ 任务处理成功",
			zap.Int("worker_id", workerID),
			zap.Int64("job_id", job.ID),
			zap.Duration("duration", duration))

		// 标记任务为完成
		if err := wp.jobQueue.MarkJobCompleted(job.ID); err != nil {
			wp.logger.Error("标记任务完成失败", zap.Error(err))
		}
	}
}

// processRedeemJob 处理兑换任务
func (wp *WorkerPool) processRedeemJob(job *Job) error {
	payload := job.Payload

	// 获取兑换码信息
	redeemCode, err := wp.redeemRepo.FindRedeemCodeByID(payload.RedeemCodeID)
	if err != nil {
		return fmt.Errorf("获取兑换码失败: %w", err)
	}
	if redeemCode == nil {
		return fmt.Errorf("兑换码不存在: %d", payload.RedeemCodeID)
	}

	// 获取活跃账号列表（如果payload没有指定账号）
	var accounts []model.Account
	if len(payload.AccountIDs) > 0 {
		// 使用指定的账号
		for _, accountID := range payload.AccountIDs {
			// 这里需要实现按ID获取账号的方法
			// 暂时先用获取所有账号然后过滤
			allAccounts, err := wp.accountRepo.GetActive()
			if err != nil {
				return fmt.Errorf("获取账号失败: %w", err)
			}
			for _, acc := range allAccounts {
				if acc.ID == accountID {
					accounts = append(accounts, acc)
					break
				}
			}
		}
	} else {
		// 获取所有活跃且已验证的账号
		allAccounts, err := wp.accountRepo.GetActive()
		if err != nil {
			return fmt.Errorf("获取活跃账号失败: %w", err)
		}
		for _, acc := range allAccounts {
			if acc.IsVerified {
				accounts = append(accounts, acc)
			}
		}
	}

	if len(accounts) == 0 {
		return fmt.Errorf("没有可用的账号")
	}

	wp.logger.Info("📦 开始批量兑换",
		zap.String("code", redeemCode.Code),
		zap.Int("accounts_count", len(accounts)))

	// 更新兑换码状态为处理中
	err = wp.redeemRepo.UpdateRedeemCodeStatus(redeemCode.ID, "processing", len(accounts))
	if err != nil {
		return fmt.Errorf("更新兑换码状态失败: %w", err)
	}

	// 转换账号格式
	clientAccounts := make([]client.Account, len(accounts))
	for i, acc := range accounts {
		clientAccounts[i] = client.Account{
			ID:  acc.ID,
			FID: acc.FID,
		}
	}

	// 执行批量兑换
	results, err := wp.automationSvc.RedeemBatch(clientAccounts, redeemCode.Code)
	if err != nil {
		return fmt.Errorf("批量兑换失败: %w", err)
	}

	// 记录兑换日志（仅在账号最终结果明确后写入一次，不在中途重试阶段写入）
	successCount := 0
	failedCount := 0

	for _, result := range results {
		var errorMessage, successMessage, captchaRecognized *string
		var processingTime, errCode *int

		if result.Error != "" {
			errorMessage = &result.Error
		}
		if result.Success {
			// 成功时写入友好提示，复刻 Node 的 success_message 行为
			msg := fmt.Sprintf("兑换成功，账号 %s 已成功兑换奖励", result.FID)
			successMessage = &msg
			successCount++
		} else {
			failedCount++
		}
		if result.CaptchaRecognized != "" {
			captchaRecognized = &result.CaptchaRecognized
		}
		if result.ProcessingTime > 0 {
			processingTime = &result.ProcessingTime
		}
		if result.ErrCode > 0 {
			errCode = &result.ErrCode
		}

		resultStr := "failed"
		if result.Success {
			resultStr = "success"
		}

		// 替换式写入兑换日志（每个账号最终结果一次）
		_, err := wp.logRepo.ReplaceRedeemLog(
			redeemCode.ID,
			result.AccountID,
			result.FID,
			redeemCode.Code,
			resultStr,
			errorMessage,
			successMessage,
			captchaRecognized,
			processingTime,
			errCode,
		)
		if err != nil {
			wp.logger.Error("创建兑换日志失败",
				zap.Error(err),
				zap.String("fid", result.FID))
		}
	}

	// 更新兑换码统计
	err = wp.redeemRepo.UpdateRedeemCodeStats(redeemCode.ID, successCount, failedCount, len(accounts))
	if err != nil {
		wp.logger.Error("更新兑换码统计失败", zap.Error(err))
	}

	// 更新兑换码状态为完成
	err = wp.redeemRepo.UpdateRedeemCodeStatus(redeemCode.ID, "completed", len(accounts))
	if err != nil {
		return fmt.Errorf("更新兑换码完成状态失败: %w", err)
	}

	wp.logger.Info("📊 兑换任务完成",
		zap.String("code", redeemCode.Code),
		zap.Int("success", successCount),
		zap.Int("failed", failedCount),
		zap.Int("total", len(accounts)))

	return nil
}

// processRetryRedeemJob 处理重试兑换任务
func (wp *WorkerPool) processRetryRedeemJob(job *Job) error {
	// 重试兑换任务与普通兑换任务类似，但可能包含特定的账号列表
	return wp.processRedeemJob(job)
}

// processSupplementRedeemJob 处理补充兑换任务
func (wp *WorkerPool) processSupplementRedeemJob(job *Job) error {
	payload := job.Payload

	// 获取兑换码信息
	redeemCode, err := wp.redeemRepo.FindRedeemCodeByID(payload.RedeemCodeID)
	if err != nil {
		return fmt.Errorf("获取兑换码失败: %w", err)
	}
	if redeemCode == nil {
		return fmt.Errorf("兑换码不存在: %d", payload.RedeemCodeID)
	}

	// 获取已参与过该兑换码的账号ID列表
	participatedAccountIDs, err := wp.logRepo.GetParticipatedAccountIDs(payload.RedeemCodeID)
	if err != nil {
		return fmt.Errorf("获取已参与账号列表失败: %w", err)
	}

	// 获取所有活跃且已验证的账号
	allAccounts, err := wp.accountRepo.GetActive()
	if err != nil {
		return fmt.Errorf("获取活跃账号失败: %w", err)
	}

	// 过滤出未参与过的账号
	participatedMap := make(map[int]bool)
	for _, id := range participatedAccountIDs {
		participatedMap[id] = true
	}

	var newAccounts []model.Account
	for _, acc := range allAccounts {
		if acc.IsVerified && !participatedMap[acc.ID] {
			newAccounts = append(newAccounts, acc)
		}
	}

	if len(newAccounts) == 0 {
		wp.logger.Info("💫 没有新账号需要补充兑换",
			zap.String("code", redeemCode.Code))
		return nil
	}

	wp.logger.Info("🔄 开始补充兑换",
		zap.String("code", redeemCode.Code),
		zap.Int("new_accounts", len(newAccounts)))

	// 转换账号格式
	clientAccounts := make([]client.Account, len(newAccounts))
	for i, acc := range newAccounts {
		clientAccounts[i] = client.Account{
			ID:  acc.ID,
			FID: acc.FID,
		}
	}

	// 执行补充兑换
	results, err := wp.automationSvc.RedeemBatch(clientAccounts, redeemCode.Code)
	if err != nil {
		return fmt.Errorf("补充兑换失败: %w", err)
	}

	// 记录兑换日志并更新统计（仅在账号最终结果明确后写入一次）
	successCount := 0
	failedCount := 0

	for _, result := range results {
		var errorMessage, successMessage, captchaRecognized *string
		var processingTime, errCode *int

		if result.Error != "" {
			errorMessage = &result.Error
		}
		if result.Success {
			// 成功时写入友好提示，复刻 Node 的 success_message 行为（补充兑换）
			msg := fmt.Sprintf("补充兑换成功，新账号 %s 已获得兑换奖励", result.FID)
			successMessage = &msg
			successCount++
		} else {
			failedCount++
		}
		if result.CaptchaRecognized != "" {
			captchaRecognized = &result.CaptchaRecognized
		}
		if result.ProcessingTime > 0 {
			processingTime = &result.ProcessingTime
		}
		if result.ErrCode > 0 {
			errCode = &result.ErrCode
		}

		resultStr := "failed"
		if result.Success {
			resultStr = "success"
		}

		// 替换式写入兑换日志（每个账号最终结果一次）
		_, err := wp.logRepo.ReplaceRedeemLog(
			redeemCode.ID,
			result.AccountID,
			result.FID,
			redeemCode.Code,
			resultStr,
			errorMessage,
			successMessage,
			captchaRecognized,
			processingTime,
			errCode,
		)
		if err != nil {
			wp.logger.Error("创建补充兑换日志失败",
				zap.Error(err),
				zap.String("fid", result.FID))
		}
	}

	// 重新计算并更新兑换码统计
	total, success, failed, err := wp.logRepo.GetLogStats(redeemCode.ID)
	if err != nil {
		wp.logger.Error("获取兑换统计失败", zap.Error(err))
	} else {
		err = wp.redeemRepo.UpdateRedeemCodeStats(redeemCode.ID, success, failed, total)
		if err != nil {
			wp.logger.Error("更新兑换码统计失败", zap.Error(err))
		}
	}

	wp.logger.Info("📊 补充兑换完成",
		zap.String("code", redeemCode.Code),
		zap.Int("success", successCount),
		zap.Int("failed", failedCount),
		zap.Int("new_total", len(newAccounts)))

	return nil
}

// monitor 监控Worker池状态
func (wp *WorkerPool) monitor() {
	defer wp.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-wp.ctx.Done():
			return
		case <-ticker.C:
			stats, err := wp.jobQueue.GetJobStats()
			if err != nil {
				wp.logger.Error("获取任务统计失败", zap.Error(err))
				continue
			}

			wp.logger.Info("📊 Worker池状态",
				zap.Int("workers", wp.concurrency),
				zap.Any("job_stats", stats))
		}
	}
}
