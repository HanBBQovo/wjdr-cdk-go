package client

import (
	"errors"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"
	"wjdr-backend-go/internal/model"

	"go.uber.org/zap"
)

// OCRRecognizer 定义识别接口，便于替换实现
type OCRRecognizer interface {
	RecognizeCaptcha(base64Image string) (string, error)
}

// weightedKey 内部结构体
type weightedKey struct {
	key        model.OCRKey
	recognizer OCRRecognizer
	current    int // 平滑加权轮询当前值
}

// OCRKeyManager 多 Key 调度器（线程安全）
type OCRKeyManager struct {
	logger *zap.Logger
	mu     sync.RWMutex
	keys   []*weightedKey
	total  int
	rnd    *rand.Rand
	// onKeyExhausted 在检测到额度问题时回调（由上层注入，负责更新DB并触发热更新）
	onKeyExhausted func(keyID int, code int, msg string)
	// onUsage 每次调用后上报一次使用统计（成功/失败）
	onUsage func(keyID int, success bool, errMsg *string)
}

func NewOCRKeyManager(logger *zap.Logger) *OCRKeyManager {
	return &OCRKeyManager{logger: logger, rnd: rand.New(rand.NewSource(time.Now().UnixNano()))}
}

// Reload 用最新 key 列表重建内部结构
func (m *OCRKeyManager) Reload(keys []model.OCRKey) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.keys = m.keys[:0]
	m.total = 0
	// 统计不同provider数量
	providerCount := map[string]int{}
	for _, k := range keys {
		if !k.IsActive || !k.HasQuota || k.Weight <= 0 {
			continue
		}
		var recognizer OCRRecognizer
		provider := strings.ToLower(strings.TrimSpace(k.Provider))
		if provider == "" {
			provider = "baidu"
		}
		if factory, ok := getOCRProvider(provider); ok {
			recognizer = factory(k.APIKey, k.SecretKey, "", m.logger)
		} else {
			// 未注册的 provider：跳过（未来可通过配置/插件注册）
			m.logger.Warn("未注册的OCR Provider，已跳过", zap.String("provider", provider))
			continue
		}
		wk := &weightedKey{key: k, recognizer: recognizer, current: 0}
		m.keys = append(m.keys, wk)
		// 剩余额度越高，实际权重可适当抬升（简易：剩余占比 * weight）
		effWeight := k.Weight
		if k.MonthlyQuota > 0 && k.RemainingQuota >= 0 {
			// 提升比例：1 + 剩余占比（最多2倍）
			eff := 1.0 + float64(k.RemainingQuota)/float64(k.MonthlyQuota)
			if eff > 2.0 {
				eff = 2.0
			}
			effWeight = int(float64(effWeight) * eff)
			if effWeight < 1 {
				effWeight = 1
			}
		}
		m.total += effWeight
		providerCount[provider]++
	}
	// 打印一次加载结果（信息级别，便于诊断）
	m.logger.Info("OCR keys reloaded", zap.Int("usable_keys", len(m.keys)), zap.Any("by_provider", providerCount))
}

// pick 使用平滑加权轮询（SWRR）选择一个 key
func (m *OCRKeyManager) pick() *weightedKey {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.keys) == 0 {
		return nil
	}
	var best *weightedKey
	for _, wk := range m.keys {
		wk.current += wk.key.Weight
		if best == nil || wk.current > best.current {
			best = wk
		}
	}
	if best != nil {
		best.current -= m.total
	}
	return best
}

// RecognizeCaptcha 多 Key 调度识别
func (m *OCRKeyManager) RecognizeCaptcha(base64Image string) (string, error) {
	// 最多尝试 len(keys) 次
	m.mu.RLock()
	tries := len(m.keys)
	m.mu.RUnlock()
	if tries == 0 {
		return "", errors.New("no usable OCR keys")
	}
	var lastErr error
	for i := 0; i < tries; i++ {
		wk := m.pick()
		if wk == nil {
			break
		}
		// 记录选择的key及provider，协助定位未命中阿里云的问题
		m.logger.Info("OCR selecting key", zap.Int("key_id", wk.key.ID), zap.String("provider", wk.key.Provider))
		result, err := wk.recognizer.RecognizeCaptcha(base64Image)
		if err == nil && result != "" {
			if m.onUsage != nil {
				m.onUsage(wk.key.ID, true, nil)
			}
			m.logger.Info("OCR recognition success", zap.Int("key_id", wk.key.ID), zap.String("provider", wk.key.Provider))
			return result, nil
		}
		lastErr = err
		if m.onUsage != nil {
			var emsg *string
			if err != nil {
				s := err.Error()
				emsg = &s
			}
			m.onUsage(wk.key.ID, false, emsg)
		}
		// 若是额度/权限相关错误，回调上层标记 has_quota=false
		if oe, ok := err.(*OCRError); ok {
			if codeInt, convErr := strconv.Atoi(oe.Code); convErr == nil {
				switch codeInt {
				// 结合 error_code.md：与额度/权限/QPS强相关的错误
				case 4, 17, 18, 19, 216604:
					if m.onKeyExhausted != nil {
						m.onKeyExhausted(wk.key.ID, codeInt, oe.Msg)
					}
				case 6, 14, 110, 111: // 权限/鉴权/token 失效
					if m.onKeyExhausted != nil {
						m.onKeyExhausted(wk.key.ID, codeInt, oe.Msg)
					}
				}
			}
		}
		// 失败则尝试下一个 key（不在这里修改 has_quota，交由上层服务判断具体错误类型后更新 DB 并触发 Reload）
	}
	if lastErr == nil {
		lastErr = errors.New("all ocr keys failed")
	}
	return "", lastErr
}

// SetOnKeyExhausted 设置额度回调
func (m *OCRKeyManager) SetOnKeyExhausted(fn func(keyID int, code int, msg string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onKeyExhausted = fn
}

// SetOnUsage 设置使用统计回调
func (m *OCRKeyManager) SetOnUsage(fn func(keyID int, success bool, errMsg *string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onUsage = fn
}
