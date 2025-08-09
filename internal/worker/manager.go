package worker

import (
	"sync"

	"wjdr-backend-go/internal/client"
	"wjdr-backend-go/internal/model"
	"wjdr-backend-go/internal/repository"

	"go.uber.org/zap"
)

// Manager Worker管理器（统一管理JobQueue和WorkerPool）
type Manager struct {
	jobQueue   *JobQueue
	workerPool *WorkerPool
	logger     *zap.Logger
	started    bool
	mu         sync.RWMutex
}

// ManagerConfig Manager配置
type ManagerConfig struct {
	QueueCapacity int // 队列容量
	Concurrency   int // Worker并发数
	RateLimitQPS  int // 外部API限流
}

func NewManager(
	config ManagerConfig,
	jobRepo *repository.JobRepository,
	automationSvc *client.AutomationService,
	accountRepo *repository.AccountRepository,
	redeemRepo *repository.RedeemRepository,
	logRepo *repository.LogRepository,
	logger *zap.Logger,
) *Manager {
	// 创建任务队列
	jobQueue := NewJobQueue(config.QueueCapacity, jobRepo, logger)

	// 创建Worker池配置
	// 强制串行，确保任意时刻仅一个账号执行兑换（与队列逻辑保持一致）
	workerConfig := WorkerPoolConfig{
		Concurrency:  1,
		RateLimitQPS: config.RateLimitQPS,
	}

	// 创建Worker池
	workerPool := NewWorkerPool(
		workerConfig,
		jobQueue,
		automationSvc,
		accountRepo,
		redeemRepo,
		logRepo,
		logger,
	)

	return &Manager{
		jobQueue:   jobQueue,
		workerPool: workerPool,
		logger:     logger,
		started:    false,
	}
}

// Start 启动Manager
func (m *Manager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return nil
	}

	m.logger.Info("🚀 启动Worker管理器")

	// 启动任务队列
	m.jobQueue.Start()

	// 启动Worker池
	m.workerPool.Start()

	m.started = true
	m.logger.Info("✅ Worker管理器启动完成")
	return nil
}

// Stop 停止Manager
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return nil
	}

	m.logger.Info("🛑 停止Worker管理器")

	// 停止Worker池
	m.workerPool.Stop()

	// 停止任务队列
	m.jobQueue.Stop()

	m.started = false
	m.logger.Info("✅ Worker管理器已停止")
	return nil
}

// IsStarted 检查是否已启动
func (m *Manager) IsStarted() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.started
}

// SubmitRedeemTask 提交兑换任务
func (m *Manager) SubmitRedeemTask(redeemCodeID int, accountIDs []int) (int64, error) {
	payload := model.JobPayload{
		RedeemCodeID: redeemCodeID,
		AccountIDs:   accountIDs,
		IsRetry:      false,
	}

	return m.jobQueue.Enqueue(JobTypeRedeem, payload, 3)
}

// SubmitRetryTask 提交重试任务
func (m *Manager) SubmitRetryTask(redeemCodeID int, accountIDs []int) (int64, error) {
	payload := model.JobPayload{
		RedeemCodeID: redeemCodeID,
		AccountIDs:   accountIDs,
		IsRetry:      true,
	}

	return m.jobQueue.Enqueue(JobTypeRetryRedeem, payload, 3)
}

// SubmitSupplementTask 提交补充兑换任务
func (m *Manager) SubmitSupplementTask(redeemCodeID int) (int64, error) {
	payload := model.JobPayload{
		RedeemCodeID: redeemCodeID,
		IsRetry:      false,
	}

	return m.jobQueue.Enqueue(JobTypeSupplementRedeem, payload, 2)
}

// GetStats 获取统计信息
func (m *Manager) GetStats() (map[string]interface{}, error) {
	jobStats, err := m.jobQueue.GetJobStats()
	if err != nil {
		return nil, err
	}

	stats := map[string]interface{}{
		"worker_manager": map[string]interface{}{
			"started":     m.IsStarted(),
			"concurrency": m.workerPool.concurrency,
			"rate_limit":  int(m.workerPool.rateLimiter.Limit()),
		},
		"job_queue": jobStats,
	}

	return stats, nil
}
