-- 无尽冬日Go版本数据库迁移脚本
-- 新增jobs表用于异步任务持久化

USE wjdr;

-- 创建异步任务表
CREATE TABLE IF NOT EXISTS jobs (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    type VARCHAR(50) NOT NULL COMMENT '任务类型：redeem, retry_redeem, supplement_redeem',
    payload JSON NOT NULL COMMENT '任务参数，包含redeem_code_id和account_ids等',
    status ENUM('pending', 'processing', 'completed', 'failed') DEFAULT 'pending',
    retries INT DEFAULT 0,
    max_retries INT DEFAULT 3,
    next_run_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    error_message TEXT NULL COMMENT '失败原因',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    
    INDEX idx_status_next_run (status, next_run_at),
    INDEX idx_type_status (type, status),
    INDEX idx_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='异步任务表';

-- 验证表是否创建成功
SELECT 'Jobs table created successfully' as message;
SHOW TABLES LIKE 'jobs';
DESCRIBE jobs;
