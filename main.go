package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"wjdr-backend-go/internal/client"
	"wjdr-backend-go/internal/config"
	"wjdr-backend-go/internal/handler"
	"wjdr-backend-go/internal/repository"
	"wjdr-backend-go/internal/service"
	"wjdr-backend-go/internal/worker"

	"github.com/gin-gonic/gin"
	rotatelogs "github.com/lestrrat-go/file-rotatelogs"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func main() {
	// 加载配置
	cfg := config.Load()

	// 初始化日志：控制台输出 + 按天滚动文件
	cfgZap := zap.NewProductionConfig()
	cfgZap.DisableStacktrace = true
	cfgZap.EncoderConfig.StacktraceKey = ""

	// 确保日志目录存在
	logDir := filepath.Join(".", "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		log.Fatalf("创建日志目录失败: %v", err)
	}
	// 每天分割日志，保留30天，创建软链 logs/app.log 指向当前
	filePattern := filepath.Join(logDir, "app-%Y-%m-%d.log")
	rotateWriter, err := rotatelogs.New(
		filePattern,
		rotatelogs.WithLinkName(filepath.Join(logDir, "app.log")),
		rotatelogs.WithRotationTime(24*time.Hour),
		rotatelogs.WithMaxAge(30*24*time.Hour),
	)
	if err != nil {
		log.Fatalf("初始化日志轮转失败: %v", err)
	}

	encoderCfg := cfgZap.EncoderConfig
	// 控制台：人类可读时间与彩色等级
	consoleEncoderCfg := encoderCfg
	consoleEncoderCfg.EncodeTime = zapcore.TimeEncoderOfLayout("2006-01-02 15:04:05.000")
	consoleEncoderCfg.EncodeLevel = zapcore.CapitalColorLevelEncoder
	consoleEncoder := zapcore.NewConsoleEncoder(consoleEncoderCfg)
	// 文件：JSON + ISO8601时间
	fileEncoderCfg := encoderCfg
	fileEncoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	fileEncoder := zapcore.NewJSONEncoder(fileEncoderCfg)
	level := zap.NewAtomicLevelAt(zap.InfoLevel)

	consoleCore := zapcore.NewCore(consoleEncoder, zapcore.AddSync(os.Stdout), level)
	fileCore := zapcore.NewCore(fileEncoder, zapcore.AddSync(rotateWriter), level)

	logger := zap.New(zapcore.NewTee(consoleCore, fileCore), zap.AddStacktrace(zap.PanicLevel))
	defer logger.Sync()

	// 设置Gin模式
	gin.SetMode(gin.ReleaseMode)

	// 创建路由
	router := gin.Default()

	// 健康检查端点
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":    "ok",
			"timestamp": time.Now().Format(time.RFC3339),
			"service":   "wjdr-backend-go",
			"version":   "1.0.0",
		})
	})

	// 基础信息端点（与Node版本保持一致）
	router.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message":    "无尽冬日兑换平台 API 服务",
			"version":    "2.0.0",
			"tech_stack": "Go + MySQL + 百度OCR",
			"endpoints": gin.H{
				"accounts": "/api/accounts",
				"redeem":   "/api/redeem",
				"admin":    "/api/admin",
			},
		})
	})

	// 初始化数据库连接
	db, err := repository.NewDatabase(&cfg.Database, logger)
	if err != nil {
		logger.Fatal("数据库初始化失败", zap.Error(err))
	}
	defer db.Close()

	// 初始化Repository
	accountRepo := repository.NewAccountRepository(db.GetDB(), logger)
	redeemRepo := repository.NewRedeemRepository(db.GetDB(), logger)
	logRepo := repository.NewLogRepository(db.GetDB(), logger)
	adminRepo := repository.NewAdminRepository(db.GetDB(), logger)
	jobRepo := repository.NewJobRepository(db.GetDB(), logger)

	// 初始化Client
	gameClient := client.NewGameClient(logger)
	// OCR 多 Key 管理器
	ocrKeyRepo := repository.NewOCRKeyRepository(db.GetDB(), logger)
	ocrKeySvc := service.NewOCRKeyService(ocrKeyRepo, logger)
	ocrManager := client.NewOCRKeyManager(logger)
	// 错误码回调：标记额度并热更新
	ocrManager.SetOnKeyExhausted(func(keyID int, code int, msg string) {
		// 将 has_quota 置为 false，并刷新内存
		if err := ocrKeySvc.MarkQuota(keyID, false); err != nil {
			logger.Warn("自动标记has_quota失败", zap.Int("key_id", keyID), zap.Int("code", code), zap.String("msg", msg), zap.Error(err))
			return
		}
		usable, err := ocrKeySvc.ListUsable()
		if err != nil {
			logger.Warn("热更新OCR Keys失败", zap.Error(err))
			return
		}
		ocrManager.Reload(usable)
		logger.Info("已自动禁用额度用尽的OCR Key", zap.Int("key_id", keyID), zap.Int("code", code))
	})
	// 统计上报：成功/失败计数
	ocrManager.SetOnUsage(func(keyID int, success bool, errMsg *string) {
		if err := ocrKeySvc.TouchUsage(keyID, success, errMsg); err != nil {
			logger.Debug("更新OCR Key使用统计失败", zap.Int("key_id", keyID), zap.Error(err))
		}
	})
	// 启动时仅加载数据库中的可用 Key（取消 ENV 兜底）
	if usable, err := ocrKeySvc.ListUsable(); err == nil {
		ocrManager.Reload(usable)
	} else {
		logger.Warn("加载OCR Keys失败", zap.Error(err))
	}
	automationSvc := client.NewAutomationService(gameClient, ocrManager, logger)

	// 初始化Worker Manager
	workerConfig := worker.ManagerConfig{
		QueueCapacity: 100,
		Concurrency:   cfg.Worker.Concurrency,
		RateLimitQPS:  cfg.Worker.RateLimitQPS,
	}
	workerManager := worker.NewManager(
		workerConfig,
		jobRepo,
		automationSvc,
		accountRepo,
		redeemRepo,
		logRepo,
		logger,
	)

	// 启动Worker Manager
	if err := workerManager.Start(); err != nil {
		logger.Fatal("启动Worker管理器失败", zap.Error(err))
	}
	defer workerManager.Stop()

	// 初始化Service（先账号与兑换服务）
	accountService := service.NewAccountService(accountRepo, gameClient, logger)
	redeemService := service.NewRedeemService(
		redeemRepo,
		accountRepo,
		logRepo,
		automationSvc,
		workerManager,
		logger,
	)

	// 供 Cron 使用的热更新函数
	reloadFunc := func() error {
		usable, err := ocrKeySvc.ListUsable()
		if err != nil {
			return err
		}
		ocrManager.Reload(usable)
		return nil
	}

	// 初始化定时任务服务（新增：账户服务、OCR服务、热更新函数）
	cronService := service.NewCronService(
		redeemRepo,
		logRepo,
		accountRepo,
		accountService,
		ocrKeySvc,
		automationSvc,
		workerManager,
		logger,
		reloadFunc,
	)
	// 初始化Admin服务（依赖cronService）
	adminService := service.NewAdminService(adminRepo, accountService, cronService, logger)

	// 启动定时任务
	if err := cronService.Start(); err != nil {
		logger.Fatal("启动定时任务失败", zap.Error(err))
	}
	defer cronService.Stop()

	// 初始化Handler
	accountHandler := handler.NewAccountHandler(accountService, logger)
	adminHandler := handler.NewAdminHandler(adminService, logger)
	// OCR Key 管理路由，所有变更后自动热更新
	ocrKeyHandler := handler.NewOCRKeyHandler(ocrKeySvc, logger, reloadFunc)
	redeemHandler := handler.NewRedeemHandler(redeemService, logger)

	// 设置中间件
	router.Use(handler.CORSMiddleware())

	// 创建认证中间件
	authMiddleware := handler.AuthMiddleware(adminService)

	// 注册API路由
	api := router.Group("/api")
	{
		// 健康检查（供反向代理下 /api/health 使用）
		api.GET("/health", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"status":    "ok",
				"timestamp": time.Now().Format(time.RFC3339),
				"service":   "wjdr-backend-go",
				"version":   "1.0.0",
			})
		})
		accountHandler.RegisterRoutes(api, authMiddleware)
		adminHandler.RegisterRoutes(api, authMiddleware)
		ocrKeyHandler.RegisterRoutes(api, authMiddleware)
		redeemHandler.RegisterRoutes(api, authMiddleware)
	}

	// 测试API端点
	router.GET("/test/db", func(c *gin.Context) {
		// 测试数据库连接
		if err := db.Ping(); err != nil {
			c.JSON(500, gin.H{"error": "数据库连接失败", "details": err.Error()})
			return
		}

		// 测试查询账号数量
		accounts, err := accountRepo.GetAll()
		if err != nil {
			c.JSON(500, gin.H{"error": "查询账号失败", "details": err.Error()})
			return
		}

		// 测试查询兑换码数量
		codes, err := redeemRepo.GetAllRedeemCodesAll()
		if err != nil {
			c.JSON(500, gin.H{"error": "查询兑换码失败", "details": err.Error()})
			return
		}

		// 获取Worker统计
		workerStats, err := workerManager.GetStats()
		if err != nil {
			logger.Error("获取Worker统计失败", zap.Error(err))
		}

		c.JSON(200, gin.H{
			"success":            true,
			"database":           "connected",
			"accounts_count":     len(accounts),
			"redeem_codes_count": len(codes),
			"worker_stats":       workerStats,
			"timestamp":          time.Now().Format(time.RFC3339),
		})
	})

	// 创建HTTP服务器
	srv := &http.Server{
		Addr:         ":" + cfg.Server.Port,
		Handler:      router,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}

	// 在goroutine中启动服务器
	go func() {
		logger.Info("🚀 服务器启动成功",
			zap.String("port", cfg.Server.Port),
			zap.String("url", fmt.Sprintf("http://localhost:%s", cfg.Server.Port)))

		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("服务器启动失败", zap.Error(err))
		}
	}()

	// 等待中断信号优雅关闭服务器
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("服务器正在关闭...")

	// 给定5秒的关闭时间
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Fatal("服务器强制关闭", zap.Error(err))
	}

	logger.Info("服务器已关闭")
}
