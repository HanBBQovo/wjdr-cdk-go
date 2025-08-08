package worker

import (
	"context"
	"fmt"
	"sync"
	"time"

	"wjdr-backend-go/internal/model"
	"wjdr-backend-go/internal/repository"

	"go.uber.org/zap"
)

// JobQueue 任务队列（内存队列 + 数据库持久化）
type JobQueue struct {
	queue       chan *Job
	repo        *repository.JobRepository
	logger      *zap.Logger
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	maxCapacity int
}

// Job 内存中的任务结构
type Job struct {
	ID         int64            `json:"id"`
	Type       string           `json:"type"`
	Payload    model.JobPayload `json:"payload"`
	Retries    int              `json:"retries"`
	MaxRetries int              `json:"max_retries"`
}

// JobType 任务类型常量
const (
	JobTypeRedeem           = "redeem"            // 兑换任务
	JobTypeRetryRedeem      = "retry_redeem"      // 重试兑换
	JobTypeSupplementRedeem = "supplement_redeem" // 补充兑换
)

func NewJobQueue(capacity int, repo *repository.JobRepository, logger *zap.Logger) *JobQueue {
	ctx, cancel := context.WithCancel(context.Background())

	return &JobQueue{
		queue:       make(chan *Job, capacity),
		repo:        repo,
		logger:      logger,
		ctx:         ctx,
		cancel:      cancel,
		maxCapacity: capacity,
	}
}

// Start 启动任务队列
func (jq *JobQueue) Start() {
	jq.wg.Add(1)
	go jq.loadPendingJobs()

	jq.logger.Info("📋 任务队列启动",
		zap.Int("capacity", jq.maxCapacity))
}

// Stop 停止任务队列
func (jq *JobQueue) Stop() {
	jq.cancel()
	jq.wg.Wait()
	close(jq.queue)
	jq.logger.Info("📋 任务队列已停止")
}

// Enqueue 入队任务
func (jq *JobQueue) Enqueue(jobType string, payload model.JobPayload, maxRetries int) (int64, error) {
	// 先持久化到数据库
	jobID, err := jq.repo.CreateJob(jobType, payload, maxRetries)
	if err != nil {
		jq.logger.Error("任务持久化失败", zap.Error(err))
		return 0, err
	}

	// 创建内存任务
	job := &Job{
		ID:         jobID,
		Type:       jobType,
		Payload:    payload,
		Retries:    0,
		MaxRetries: maxRetries,
	}

	// 非阻塞入队
	select {
	case jq.queue <- job:
		jq.logger.Info("✅ 任务入队成功",
			zap.Int64("job_id", jobID),
			zap.String("type", jobType))
		return jobID, nil
	default:
		// 队列满了，标记任务为pending等待后续加载
		jq.logger.Warn("⚠️ 队列已满，任务将等待处理",
			zap.Int64("job_id", jobID),
			zap.String("type", jobType))
		return jobID, nil
	}
}

// Dequeue 出队任务
func (jq *JobQueue) Dequeue() <-chan *Job {
	return jq.queue
}

// MarkJobProcessing 标记任务为处理中
func (jq *JobQueue) MarkJobProcessing(jobID int64) error {
	return jq.repo.MarkJobProcessing(jobID)
}

// MarkJobCompleted 标记任务为已完成
func (jq *JobQueue) MarkJobCompleted(jobID int64) error {
	return jq.repo.MarkJobCompleted(jobID)
}

// MarkJobFailed 标记任务为失败
func (jq *JobQueue) MarkJobFailed(jobID int64, errorMessage string) error {
	return jq.repo.MarkJobFailed(jobID, errorMessage)
}

// RetryJob 重试任务
func (jq *JobQueue) RetryJob(job *Job, errorMessage string) error {
	if job.Retries >= job.MaxRetries {
		// 达到最大重试次数，标记为失败
		return jq.MarkJobFailed(job.ID, fmt.Sprintf("达到最大重试次数: %s", errorMessage))
	}

	// 指数退避策略（1s, 2s, 4s, 8s...最大60s）
	delay := time.Duration(1<<job.Retries) * time.Second
	if delay > 60*time.Second {
		delay = 60 * time.Second
	}

	nextRunAt := time.Now().Add(delay)

	// 更新数据库中的重试信息
	err := jq.repo.IncrementJobRetries(job.ID, nextRunAt, errorMessage)
	if err != nil {
		return err
	}

	jq.logger.Info("🔄 任务将重试",
		zap.Int64("job_id", job.ID),
		zap.Int("retries", job.Retries+1),
		zap.Int("max_retries", job.MaxRetries),
		zap.Duration("delay", delay))

	return nil
}

// GetQueueLength 获取队列长度
func (jq *JobQueue) GetQueueLength() int {
	return len(jq.queue)
}

// GetQueueCapacity 获取队列容量
func (jq *JobQueue) GetQueueCapacity() int {
	return jq.maxCapacity
}

// loadPendingJobs 从数据库加载待处理的任务到内存队列
func (jq *JobQueue) loadPendingJobs() {
	defer jq.wg.Done()

	ticker := time.NewTicker(10 * time.Second) // 每10秒检查一次
	defer ticker.Stop()

	for {
		select {
		case <-jq.ctx.Done():
			return
		case <-ticker.C:
			jq.loadBatch()
		}
	}
}

// loadBatch 批量加载任务
func (jq *JobQueue) loadBatch() {
	// 计算需要加载的任务数量
	currentLength := len(jq.queue)
	availableSpace := jq.maxCapacity - currentLength

	if availableSpace <= 0 {
		return // 队列已满
	}

	// 从数据库获取待处理的任务
	jobs, err := jq.repo.GetPendingJobs(availableSpace)
	if err != nil {
		jq.logger.Error("加载待处理任务失败", zap.Error(err))
		return
	}

	if len(jobs) == 0 {
		return // 没有待处理的任务
	}

	// 将任务加载到内存队列
	loadedCount := 0
	for _, dbJob := range jobs {
		// 解析任务载荷
		payload, err := jq.repo.ParseJobPayload(dbJob.Payload)
		if err != nil {
			jq.logger.Error("解析任务载荷失败",
				zap.Int64("job_id", dbJob.ID),
				zap.Error(err))
			continue
		}

		job := &Job{
			ID:         dbJob.ID,
			Type:       dbJob.Type,
			Payload:    *payload,
			Retries:    dbJob.Retries,
			MaxRetries: dbJob.MaxRetries,
		}

		// 非阻塞入队
		select {
		case jq.queue <- job:
			loadedCount++
		default:
			// 队列满了，停止加载
			break
		}
	}

	if loadedCount > 0 {
		jq.logger.Info("📥 从数据库加载任务到队列",
			zap.Int("loaded", loadedCount),
			zap.Int("available", len(jobs)))
	}
}

// GetJobStats 获取任务统计信息
func (jq *JobQueue) GetJobStats() (map[string]int, error) {
	dbStats, err := jq.repo.GetJobStats()
	if err != nil {
		return nil, err
	}

	// 添加内存队列信息
	dbStats["queue_length"] = jq.GetQueueLength()
	dbStats["queue_capacity"] = jq.GetQueueCapacity()

	return dbStats, nil
}
