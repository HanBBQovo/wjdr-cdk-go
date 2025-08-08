package repository

import (
	"database/sql"
	"encoding/json"
	"time"
	"wjdr-backend-go/internal/model"

	"go.uber.org/zap"
)

type JobRepository struct {
	db     *sql.DB
	logger *zap.Logger
}

func NewJobRepository(db *sql.DB, logger *zap.Logger) *JobRepository {
	return &JobRepository{
		db:     db,
		logger: logger,
	}
}

// CreateJob 创建新任务
func (r *JobRepository) CreateJob(jobType string, payload model.JobPayload, maxRetries int) (int64, error) {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		r.logger.Error("序列化任务载荷失败", zap.Error(err))
		return 0, err
	}

	query := `
		INSERT INTO jobs (type, payload, status, retries, max_retries, next_run_at) 
		VALUES (?, ?, 'pending', 0, ?, NOW())
	`

	result, err := r.db.Exec(query, jobType, string(payloadJSON), maxRetries)
	if err != nil {
		r.logger.Error("创建任务失败",
			zap.Error(err),
			zap.String("type", jobType),
			zap.Any("payload", payload))
		return 0, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}

	r.logger.Info("任务创建成功",
		zap.Int64("id", id),
		zap.String("type", jobType),
		zap.Any("payload", payload))

	return id, nil
}

// GetPendingJobs 获取待处理的任务
func (r *JobRepository) GetPendingJobs(limit int) ([]model.Job, error) {
	query := `
		SELECT id, type, payload, status, retries, max_retries, next_run_at, error_message, created_at, updated_at
		FROM jobs 
		WHERE status = 'pending' AND next_run_at <= NOW()
		ORDER BY created_at ASC
		LIMIT ?
	`

	rows, err := r.db.Query(query, limit)
	if err != nil {
		r.logger.Error("查询待处理任务失败", zap.Error(err))
		return nil, err
	}
	defer rows.Close()

	var jobs []model.Job
	for rows.Next() {
		var job model.Job
		err := rows.Scan(
			&job.ID,
			&job.Type,
			&job.Payload,
			&job.Status,
			&job.Retries,
			&job.MaxRetries,
			&job.NextRunAt,
			&job.ErrorMessage,
			&job.CreatedAt,
			&job.UpdatedAt,
		)
		if err != nil {
			r.logger.Error("扫描任务数据失败", zap.Error(err))
			return nil, err
		}
		jobs = append(jobs, job)
	}

	return jobs, nil
}

// UpdateJobStatus 更新任务状态
func (r *JobRepository) UpdateJobStatus(id int64, status string, errorMessage *string) error {
	query := `UPDATE jobs SET status = ?, error_message = ?, updated_at = NOW() WHERE id = ?`

	_, err := r.db.Exec(query, status, errorMessage, id)
	if err != nil {
		r.logger.Error("更新任务状态失败",
			zap.Error(err),
			zap.Int64("id", id),
			zap.String("status", status))
		return err
	}

	return nil
}

// MarkJobProcessing 标记任务为处理中
func (r *JobRepository) MarkJobProcessing(id int64) error {
	return r.UpdateJobStatus(id, "processing", nil)
}

// MarkJobCompleted 标记任务为已完成
func (r *JobRepository) MarkJobCompleted(id int64) error {
	return r.UpdateJobStatus(id, "completed", nil)
}

// MarkJobFailed 标记任务为失败
func (r *JobRepository) MarkJobFailed(id int64, errorMessage string) error {
	return r.UpdateJobStatus(id, "failed", &errorMessage)
}

// IncrementJobRetries 增加任务重试次数
func (r *JobRepository) IncrementJobRetries(id int64, nextRunAt time.Time, errorMessage string) error {
	query := `
		UPDATE jobs 
		SET retries = retries + 1, next_run_at = ?, error_message = ?, status = 'pending', updated_at = NOW()
		WHERE id = ?
	`

	_, err := r.db.Exec(query, nextRunAt, errorMessage, id)
	if err != nil {
		r.logger.Error("增加任务重试次数失败",
			zap.Error(err),
			zap.Int64("id", id))
		return err
	}

	r.logger.Info("任务重试次数已增加",
		zap.Int64("id", id),
		zap.Time("next_run_at", nextRunAt))

	return nil
}

// GetJobByID 通过ID获取任务
func (r *JobRepository) GetJobByID(id int64) (*model.Job, error) {
	query := `
		SELECT id, type, payload, status, retries, max_retries, next_run_at, error_message, created_at, updated_at
		FROM jobs WHERE id = ?
	`

	row := r.db.QueryRow(query, id)

	var job model.Job
	err := row.Scan(
		&job.ID,
		&job.Type,
		&job.Payload,
		&job.Status,
		&job.Retries,
		&job.MaxRetries,
		&job.NextRunAt,
		&job.ErrorMessage,
		&job.CreatedAt,
		&job.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		r.logger.Error("查询任务失败", zap.Error(err), zap.Int64("id", id))
		return nil, err
	}

	return &job, nil
}

// ParseJobPayload 解析任务载荷
func (r *JobRepository) ParseJobPayload(payloadJSON string) (*model.JobPayload, error) {
	var payload model.JobPayload
	err := json.Unmarshal([]byte(payloadJSON), &payload)
	if err != nil {
		r.logger.Error("解析任务载荷失败", zap.Error(err), zap.String("payload", payloadJSON))
		return nil, err
	}
	return &payload, nil
}

// CleanOldJobs 清理旧任务（可选的维护操作）
func (r *JobRepository) CleanOldJobs(olderThan time.Duration) (int, error) {
	cutoffTime := time.Now().Add(-olderThan)

	query := `DELETE FROM jobs WHERE status IN ('completed', 'failed') AND updated_at < ?`

	result, err := r.db.Exec(query, cutoffTime)
	if err != nil {
		r.logger.Error("清理旧任务失败", zap.Error(err))
		return 0, err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}

	if rowsAffected > 0 {
		r.logger.Info("清理旧任务成功", zap.Int64("count", rowsAffected))
	}

	return int(rowsAffected), nil
}

// GetJobStats 获取任务统计信息
func (r *JobRepository) GetJobStats() (map[string]int, error) {
	query := `
		SELECT status, COUNT(*) as count 
		FROM jobs 
		GROUP BY status
	`

	rows, err := r.db.Query(query)
	if err != nil {
		r.logger.Error("查询任务统计失败", zap.Error(err))
		return nil, err
	}
	defer rows.Close()

	stats := make(map[string]int)
	for rows.Next() {
		var status string
		var count int
		err := rows.Scan(&status, &count)
		if err != nil {
			return nil, err
		}
		stats[status] = count
	}

	return stats, nil
}
