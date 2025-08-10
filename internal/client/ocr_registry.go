package client

import (
	"strings"

	"go.uber.org/zap"
)

// ProviderFactory 工厂方法：根据 apiKey/secret/region 构造 OCRRecognizer
type ProviderFactory func(apiKey, secret, region string, logger *zap.Logger) OCRRecognizer

var providerFactories = map[string]ProviderFactory{}

// RegisterOCRProvider 注册一个新的 OCR Provider 工厂
func RegisterOCRProvider(name string, factory ProviderFactory) {
	key := strings.ToLower(strings.TrimSpace(name))
	if key == "" || factory == nil {
		return
	}
	providerFactories[key] = factory
}

func getOCRProvider(name string) (ProviderFactory, bool) {
	key := strings.ToLower(strings.TrimSpace(name))
	f, ok := providerFactories[key]
	return f, ok
}

// 预注册内置 Provider：仅 baidu
func init() {
	RegisterOCRProvider("baidu", func(apiKey, secret, region string, logger *zap.Logger) OCRRecognizer {
		return NewOCRClient(apiKey, secret, logger)
	})
}
