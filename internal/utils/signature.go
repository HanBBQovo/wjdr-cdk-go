package utils

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
)

// GenerateAccountSign 生成添加账号的签名（SHA256前32位）
// 与现有GameClient的generateSign逻辑保持一致，但使用SHA256
func GenerateAccountSign(fid, timestamp, salt string) string {
	params := map[string]string{
		"fid":       fid,
		"timestamp": timestamp,
	}

	// 对参数键进行字母顺序排序（与GameClient逻辑一致）
	var sortedKeys []string
	for key := range params {
		sortedKeys = append(sortedKeys, key)
	}
	sort.Strings(sortedKeys)

	// 构建参数字符串
	var sortedParams []string
	for _, key := range sortedKeys {
		sortedParams = append(sortedParams, fmt.Sprintf("%s=%s", key, params[key]))
	}

	signString := strings.Join(sortedParams, "&") + salt

	// 生成SHA256哈希并取前32位
	hash := sha256.Sum256([]byte(signString))
	fullHash := fmt.Sprintf("%x", hash)

	// 截取前32位
	return fullHash[:32]
}

// VerifyAccountSign 验证添加账号的签名
func VerifyAccountSign(fid, timestamp, providedSign, salt string) bool {
	expectedSign := GenerateAccountSign(fid, timestamp, salt)
	return expectedSign == providedSign
}
