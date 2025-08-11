package service

import (
	"encoding/xml"
	"html"
	"net/http"
	"regexp"
	"strings"
	"time"

	"wjdr-backend-go/internal/client"
	"wjdr-backend-go/internal/model"
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
	redeemSvc     *RedeemService
	rssRepo       *repository.RSSRepository
	ocrKeySvc     *OCRKeyService
	automationSvc *client.AutomationService
	workerManager *worker.Manager
	logger        *zap.Logger
	reloadOCRKeys func() error
	feedURL       string
	updateURL     string
}

func NewCronService(
	redeemRepo *repository.RedeemRepository,
	logRepo *repository.LogRepository,
	accountRepo *repository.AccountRepository,
	accountSvc *AccountService,
	redeemSvc *RedeemService,
	rssRepo *repository.RSSRepository,
	ocrKeySvc *OCRKeyService,
	automationSvc *client.AutomationService,
	workerManager *worker.Manager,
	logger *zap.Logger,
	reloadOCRKeys func() error,
	feedURL string,
	updateURL string,
) *CronService {
	// 创建cron实例，使用秒级精度 + 本地时区
	c := cron.New(cron.WithSeconds(), cron.WithLocation(time.Local))

	return &CronService{
		cron:          c,
		redeemRepo:    redeemRepo,
		logRepo:       logRepo,
		accountSvc:    accountSvc,
		redeemSvc:     redeemSvc,
		automationSvc: automationSvc,
		accountRepo:   accountRepo,
		rssRepo:       rssRepo,
		workerManager: workerManager,
		logger:        logger,
		ocrKeySvc:     ocrKeySvc,
		reloadOCRKeys: reloadOCRKeys,
		feedURL:       feedURL,
		updateURL:     updateURL,
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

	// 5. RSS 抓取：每天11:02、16:02、20:02执行
	if s.feedURL != "" && s.rssRepo != nil && s.redeemSvc != nil {
		if _, err = s.cron.AddFunc("0 2 11 * * *", s.FetchAndProcessRSS); err != nil {
			s.logger.Error("添加RSS(11:02)任务失败", zap.Error(err))
			return err
		}
		if _, err = s.cron.AddFunc("0 2 16 * * *", s.FetchAndProcessRSS); err != nil {
			s.logger.Error("添加RSS(16:02)任务失败", zap.Error(err))
			return err
		}
		if _, err = s.cron.AddFunc("0 2 20 * * *", s.FetchAndProcessRSS); err != nil {
			s.logger.Error("添加RSS(20:02)任务失败", zap.Error(err))
			return err
		}
	}

	// 启动cron
	s.cron.Start()

	s.logger.Info("✅ 定时任务服务启动成功")
	s.logger.Info("📅 定时任务计划:")
	s.logger.Info("  - 00:00 清理过期兑换码")
	s.logger.Info("  - 00:10 自动补充兑换")
	s.logger.Info("  - 00:00(每月1日) 重置OCR Key额度")
	s.logger.Info("  - 03:00 刷新所有用户数据")
	if s.feedURL != "" && s.rssRepo != nil && s.redeemSvc != nil {
		s.logger.Info("  - 11:02 RSS 抓取并处理兑换码")
		s.logger.Info("  - 16:02 RSS 抓取并处理兑换码")
		s.logger.Info("  - 20:02 RSS 抓取并处理兑换码")
	}

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
			time.Sleep(3 * time.Second)
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

// ListProcessedArticles 最近已处理文章（供前端展示）
func (s *CronService) ListProcessedArticles(limit int) ([]model.ProcessedArticle, error) {
	if s.rssRepo == nil {
		return []model.ProcessedArticle{}, nil
	}
	if limit <= 0 {
		return s.rssRepo.ListProcessedArticlesAll()
	}
	return s.rssRepo.ListProcessedArticles(limit)
}

// FetchAndProcessRSS 拉取RSS并解析可能包含兑换码的文章
func (s *CronService) FetchAndProcessRSS() {
	if s.feedURL == "" {
		s.logger.Warn("RSS feedURL 未配置，跳过")
		return
	}
	s.logger.Info("📰 开始RSS抓取", zap.String("url", s.feedURL))

	// 拉取
	req, _ := http.NewRequest("GET", s.feedURL, nil)
	// 部分源站对UA敏感，补充常见UA；同时提高超时以适配较大内容
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; RSSFetcher/1.0; +https://example.com)")
	req.Header.Set("Accept", "application/atom+xml, application/xml, text/xml;q=0.9, */*;q=0.8")
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		s.logger.Error("RSS 请求失败", zap.Error(err))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		s.logger.Error("RSS 状态码异常", zap.Int("status", resp.StatusCode))
		return
	}

	// 仅解析我们需要的字段
	type entry struct {
		ID    string `xml:"id"`
		Title string `xml:"title"`
		Link  struct {
			Href string `xml:"href,attr"`
		} `xml:"link"`
		Content string `xml:"content"`
		Updated string `xml:"updated"`
	}
	var feed struct {
		XMLName xml.Name `xml:"feed"`
		Entries []entry  `xml:"entry"`
	}
	dec := xml.NewDecoder(resp.Body)
	dec.Strict = false
	if err := dec.Decode(&feed); err != nil {
		s.logger.Error("RSS 解析失败", zap.Error(err))
		return
	}

	// 关键词判断：标题含“内含兑换码”或“兑换码”才解析正文
	hasKeyword := func(title string) bool {
		t := strings.TrimSpace(title)
		return strings.Contains(t, "内含兑换码") || strings.Contains(t, "兑换码")
	}

	// 从HTML正文提取兑换码：先去标签/解码实体，再匹配
	stripHTML := func(in string) string {
		reBr := regexp.MustCompile(`(?i)<\s*(br|/p|/div)\s*>`)
		in = reBr.ReplaceAllString(in, "\n")
		reTag := regexp.MustCompile(`<[^>]+>`) // 去标签
		in = reTag.ReplaceAllString(in, " ")
		in = html.UnescapeString(in)
		in = strings.ReplaceAll(in, "\u00A0", " ")
		ws := regexp.MustCompile(`\s+`)
		in = ws.ReplaceAllString(in, " ")
		return strings.TrimSpace(in)
	}
	codeRe := regexp.MustCompile(`(?i)(?:兑换码|code)\s*[:：]?\s*([A-Za-z0-9-]{4,32})`)
	nearTokenRe := regexp.MustCompile(`([A-Za-z0-9-]{4,32})`)

	processed := 0
	created := 0
	for _, e := range feed.Entries {
		if e.ID == "" {
			continue
		}
		ok, err := s.rssRepo.IsProcessed(e.ID)
		if err != nil {
			s.logger.Warn("查询processed失败，跳过", zap.Error(err))
			continue
		}
		if ok {
			continue
		}
		processed++

		if !hasKeyword(e.Title) {
			// 标题不含关键词也标记已处理，避免重复
			_ = s.rssRepo.MarkProcessed(e.ID, e.Title, e.Link.Href)
			continue
		}

		// 提取兑换码
		text := stripHTML(e.Content)
		matches := codeRe.FindAllStringSubmatch(text, -1)
		if len(matches) == 0 {
			// 在出现“兑换码/Code”后短窗口内寻找
			up := strings.ToUpper(text)
			idx := strings.Index(up, "兑换码")
			if idx < 0 {
				idx = strings.Index(up, "CODE")
			}
			if idx >= 0 {
				end := idx + 120
				if end > len(text) {
					end = len(text)
				}
				win := text[idx:end]
				matches = nearTokenRe.FindAllStringSubmatch(win, -1)
			}
			if len(matches) == 0 {
				_ = s.rssRepo.MarkProcessed(e.ID, e.Title, e.Link.Href)
				continue
			}
		}

		// 去重
		seen := make(map[string]struct{})
		extracted := make([]string, 0)
		for _, m := range matches {
			if len(m) < 2 {
				continue
			}
			code := strings.ToUpper(strings.TrimSpace(m[1]))
			if len(code) < 4 || len(code) > 64 {
				continue
			}
			if _, exists := seen[code]; exists {
				continue
			}
			seen[code] = struct{}{}
			extracted = append(extracted, code)

			// 提交到兑换流程（内部会验证是否有效与是否已存在）
			res, err := s.redeemSvc.SubmitRedeemCode(code, false)
			if err != nil {
				s.logger.Warn("提交兑换码失败", zap.String("code", code), zap.Error(err))
				continue
			}
			if res != nil && res.Success {
				created++
				s.logger.Info("已从RSS创建兑换码并触发处理", zap.String("code", code))
			} else {
				s.logger.Info("RSS兑换码未创建", zap.String("code", code), zap.String("error", res.Error))
			}
		}

		if len(extracted) > 0 {
			s.logger.Info("RSS提取到兑换码", zap.String("title", e.Title), zap.Strings("codes", extracted))
			// 标记文章已处理（含codes）
			if err := s.rssRepo.MarkProcessedWithCodes(e.ID, e.Title, e.Link.Href, extracted); err != nil {
				s.logger.Warn("标记processed含codes失败，降级为无codes", zap.Error(err))
				_ = s.rssRepo.MarkProcessed(e.ID, e.Title, e.Link.Href)
			}
			continue
		}

		// 无提取结果：仅标记文章已处理
		if err := s.rssRepo.MarkProcessed(e.ID, e.Title, e.Link.Href); err != nil {
			s.logger.Warn("标记processed失败", zap.Error(err))
		}
	}

	s.logger.Info("✅ RSS处理完成", zap.Int("entries_checked", len(feed.Entries)), zap.Int("entries_processed", processed), zap.Int("codes_created", created))
}

// FetchAndProcessRSSManual 手动抓取：先调用更新URL，再等待固定时长后抓取RSS
func (s *CronService) FetchAndProcessRSSManual() {
	// 1) 先更新源站内容
	if strings.TrimSpace(s.updateURL) != "" {
		s.logger.Info("🔄 触发RSS源更新", zap.String("url", s.updateURL))
		req, _ := http.NewRequest("GET", s.updateURL, nil)
		req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; RSSUpdater/1.0)")
		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			s.logger.Warn("RSS 更新请求失败", zap.Error(err))
		} else {
			_ = resp.Body.Close()
			s.logger.Info("✅ RSS 更新请求完成", zap.Int("status", resp.StatusCode))
		}
	} else {
		s.logger.Warn("未配置 RSS 更新URL，跳过更新步骤")
	}

	// 2) 等待约10秒
	s.logger.Info("⏳ 等待10秒后开始抓取RSS…")
	time.Sleep(10 * time.Second)

	// 3) 执行常规抓取
	s.FetchAndProcessRSS()
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
