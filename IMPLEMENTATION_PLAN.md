# Go 重构实施计划

## 核心原则

- **API 完全兼容**：所有端点的请求/响应格式与 Node 版本一致
- **功能完全对齐**：业务逻辑、错误处理、统计计算完全一致
- **性能优化**：通过异步任务和并发提升性能，但不改变对外行为

## 详细实施清单

### 1. 项目初始化

- 创建 go.mod，引入依赖
- 建立目录结构：cmd/server, internal/{handler,service,repository,client,worker,config}
- 配置管理：读取环境变量（端口 6382 等）
- 日志系统：zap 结构化日志
- 数据库连接池：复用现有数据库 wjdr_platform

### 2. Repository 层实现

- 复用现有表：game_accounts, redeem_codes, redeem_logs, admin_passwords, admin_tokens
- 新增 jobs 表：持久化异步任务
- 实现所有现有 SQL 查询，保持查询结果格式一致
- 事务支持和连接池管理

### 3. 外部 Client 实现

- GameClient：完全复刻 sign 算法和 API 调用逻辑
- OCRClient：复刻百度 OCR 高精度版本调用
- 错误码映射：与 Node 版本完全一致
- 超时和重试：复刻 Node 的重试逻辑

### 4. 异步任务系统

- Job 模型：包含 redeem_code_id, account_ids, type 等
- JobQueue：基于 channel + jobs 表持久化
- WorkerPool：可配置并发度，默认优化但可降级为单线程
- 幂等性：防止重复处理同一任务

### 5. Service 层业务逻辑

- AccountService：与 Node 的 Account 模型完全对齐
- RedeemService：核心兑换逻辑，processRedeemCode 函数对齐
- AdminService：密码验证和 token 管理对齐
- 统计计算：成功/失败计数逻辑完全一致

### 6. Handler 层 API

- /api/accounts/\*：请求/响应格式完全一致
- /api/redeem/\*：POST 改为异步但返回格式不变，GET 查询完全一致
- /api/admin/\*：token 验证和管理完全一致
- 错误响应格式：与 Node 版本一致

### 7. 定时任务

- 复刻 cronJobs.js 的两个定时任务
- 清理过期兑换码：逻辑和时间完全一致
- 自动补充兑换：逻辑完全一致，但改为创建异步任务

### 8. 配置和部署

- 环境变量：复用现有.env 配置
- 启动脚本：Makefile 和 docker 支持
- 健康检查：/health 端点
- 优雅关闭：处理 SIGTERM 信号

### 9. 测试验证

- 单元测试：核心逻辑测试
- API 测试：确保与 Node 响应格式一致
- 性能测试：验证异步处理的性能提升
- 集成测试：确保前端可直接切换

## 数据库迁移 SQL

```sql
-- 新增jobs表用于任务持久化
CREATE TABLE jobs (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    type VARCHAR(50) NOT NULL COMMENT '任务类型：redeem, retry_redeem',
    payload JSON NOT NULL COMMENT '任务参数',
    status ENUM('pending', 'processing', 'completed', 'failed') DEFAULT 'pending',
    retries INT DEFAULT 0,
    max_retries INT DEFAULT 3,
    next_run_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    INDEX idx_status_next_run (status, next_run_at),
    INDEX idx_type_status (type, status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='异步任务表';
```

## 验收标准

- [ ] 所有 API 端点返回格式与 Node 一致
- [ ] 前端无需任何修改即可切换到 Go 版本
- [ ] 补充兑换性能显著提升（2 账号 ×20 码场景）
- [ ] 错误处理和日志格式保持一致
- [ ] 统计数据计算结果一致
