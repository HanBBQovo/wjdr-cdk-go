package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"wjdr-backend-go/internal/client"
	"wjdr-backend-go/internal/config"
	"wjdr-backend-go/internal/handler"
	"wjdr-backend-go/internal/repository"
	"wjdr-backend-go/internal/service"
	"wjdr-backend-go/internal/worker"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func main() {
	// 加载配置
	cfg := config.Load()

	// 初始化日志
	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("初始化日志失败: %v", err)
	}
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
	ocrClient := client.NewOCRClient(cfg.OCR.BaiduAPIKey, cfg.OCR.BaiduSecretKey, logger)
	automationSvc := client.NewAutomationService(gameClient, ocrClient, logger)

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

	// 初始化Service
	accountService := service.NewAccountService(accountRepo, gameClient, logger)
	adminService := service.NewAdminService(adminRepo, accountService, logger)
	redeemService := service.NewRedeemService(
		redeemRepo,
		accountRepo,
		logRepo,
		automationSvc,
		workerManager,
		logger,
	)

	// 初始化定时任务服务
	cronService := service.NewCronService(
		redeemRepo,
		logRepo,
		automationSvc,
		workerManager,
		logger,
	)

	// 启动定时任务
	if err := cronService.Start(); err != nil {
		logger.Fatal("启动定时任务失败", zap.Error(err))
	}
	defer cronService.Stop()

	// 初始化Handler
	accountHandler := handler.NewAccountHandler(accountService, logger)
	adminHandler := handler.NewAdminHandler(adminService, logger)
	redeemHandler := handler.NewRedeemHandler(redeemService, logger)

	// 设置中间件
	router.Use(handler.CORSMiddleware())

	// 创建认证中间件
	authMiddleware := handler.AuthMiddleware(adminService)

	// 注册API路由
	api := router.Group("/api")
	{
		accountHandler.RegisterRoutes(api, authMiddleware)
		adminHandler.RegisterRoutes(api, authMiddleware)
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
