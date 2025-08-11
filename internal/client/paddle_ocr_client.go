package client

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"go.uber.org/zap"
)

// PaddleOCRClient 基于本地 PaddleOCR 脚本的识别器
// 通过调用 predict_rec.py 脚本识别单张验证码图片
// 依赖环境变量（可选）覆盖默认路径：
// - PADDLE_OCR_PYTHON: python 可执行，默认 python3
// - PADDLE_OCR_SCRIPT: predict_rec.py 绝对/相对路径（默认 ./third_party/PaddleOCR/tools/infer/predict_rec.py）
// - PADDLE_OCR_MODEL_DIR: 模型目录（默认 ./third_party/wjdr_OCR/output/rec_crnn/infer_0811_0501）
// - PADDLE_OCR_CHAR_DICT: 字典路径（默认 ./third_party/wjdr_OCR/data/captcha_rec/dict_cap36.txt）
// - PADDLE_OCR_IMAGE_SHAPE: 例如 3,32,160（默认 3,32,160）
// - PADDLE_OCR_EXTRA_ARGS: 其他附加参数（可为空）
// - PADDLE_OCR_TIMEOUT_MS: 识别超时（默认 15000）
// 临时图片将写入 ./tmp/ocr 目录

type PaddleOCRClient struct {
	pythonExe  string
	scriptPath string
	modelDir   string
	charDict   string
	imageShape string
	extraArgs  []string
	timeout    time.Duration
	logger     *zap.Logger
}

func NewPaddleOCRClient(logger *zap.Logger) *PaddleOCRClient {
	cwd, _ := os.Getwd()
	resolve := func(p string) string {
		if p == "" {
			return p
		}
		if filepath.IsAbs(p) {
			return p
		}
		return filepath.Clean(filepath.Join(cwd, p))
	}

	python := getenvDefault("PADDLE_OCR_PYTHON", "python3")
	script := getenvDefault("PADDLE_OCR_SCRIPT", "./third_party/PaddleOCR/tools/infer/predict_rec.py")
	model := getenvDefault("PADDLE_OCR_MODEL_DIR", "./third_party/wjdr_OCR/output")
	charD := getenvDefault("PADDLE_OCR_CHAR_DICT", "./third_party/wjdr_OCR/data/dict_cap36.txt")
	shape := getenvDefault("PADDLE_OCR_IMAGE_SHAPE", "3,32,160")
	extra := strings.TrimSpace(os.Getenv("PADDLE_OCR_EXTRA_ARGS"))
	timeoutMs := getenvDefault("PADDLE_OCR_TIMEOUT_MS", "15000")
	to := 15000
	if n, err := strconvAtoiSafe(timeoutMs); err == nil && n > 0 {
		to = n
	}

	client := &PaddleOCRClient{
		pythonExe:  python,
		scriptPath: resolve(script),
		modelDir:   resolve(model),
		charDict:   resolve(charD),
		imageShape: shape,
		extraArgs:  splitArgs(extra),
		timeout:    time.Duration(to) * time.Millisecond,
		logger:     logger,
	}
	return client
}

func getenvDefault(key, def string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	return v
}

func splitArgs(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	// 简单拆分，建议不要包含带空格的参数值
	return strings.Fields(s)
}

func strconvAtoiSafe(s string) (int, error) {
	var n int
	_, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &n)
	if err != nil {
		return 0, err
	}
	return n, nil
}

func (c *PaddleOCRClient) RecognizeCaptcha(base64Image string) (string, error) {
	// 写临时文件
	imgBytes, err := c.decodeBase64Image(base64Image)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll("./tmp/ocr", 0o755); err != nil {
		return "", err
	}
	name := fmt.Sprintf("tmp_%d_%04d.png", time.Now().UnixNano()/1e6, rand.Intn(10000))
	imgPath := filepath.Join("./tmp/ocr", name)
	if err := os.WriteFile(imgPath, imgBytes, 0o644); err != nil {
		return "", err
	}
	defer os.Remove(imgPath)

	// 运行命令
	args := []string{
		c.scriptPath,
		"--use_gpu=False",
		"--rec_algorithm=CRNN",
		"--rec_model_dir", c.modelDir,
		"--rec_char_dict_path", c.charDict,
		"--rec_image_shape", c.imageShape,
		"--image_dir", imgPath,
	}
	if len(c.extraArgs) > 0 {
		args = append(args, c.extraArgs...)
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.pythonExe, args...)
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		c.logger.Error("PaddleOCR 调用超时")
		return "", errors.New("paddle ocr timeout")
	}
	if err != nil {
		c.logger.Error("PaddleOCR 调用失败", zap.Error(err), zap.String("output", string(out)))
		return "", fmt.Errorf("paddle ocr failed: %v", err)
	}

	text := parsePaddleOutput(string(out))
	if text == "" {
		c.logger.Warn("PaddleOCR 输出无法解析", zap.String("output", string(out)))
		return "", errors.New("paddle ocr parse failed")
	}
	// 规范为4位
	norm := normalizeTo4(text)
	if len(norm) != 4 {
		return "", fmt.Errorf("验证码长度异常: %s", norm)
	}
	return norm, nil
}

func (c *PaddleOCRClient) decodeBase64Image(s string) ([]byte, error) {
	ss := strings.TrimSpace(s)
	if i := strings.Index(ss, ","); i > 0 && strings.Contains(strings.ToLower(ss[:i]), "base64") {
		ss = ss[i+1:]
	}
	ss = strings.ReplaceAll(ss, "\n", "")
	ss = strings.ReplaceAll(ss, "\r", "")
	ss = strings.TrimSpace(ss)
	return base64.StdEncoding.DecodeString(ss)
}

var (
	rePredict = regexp.MustCompile(`Predicts of .*?:\('([A-Za-z0-9]{3,12})'\s*,\s*[0-9.]+\)`) // ('XXXX', 0.99)
	reText    = regexp.MustCompile(`[\"']text[\"']\s*[:=]\s*[\"']([A-Za-z0-9]{3,12})[\"']`)
	reAny     = regexp.MustCompile(`([A-Za-z0-9]{3,12})`)
)

func parsePaddleOutput(out string) string {
	if m := rePredict.FindStringSubmatch(out); len(m) == 2 {
		return m[1]
	}
	if m := reText.FindStringSubmatch(out); len(m) == 2 {
		return m[1]
	}
	if ms := reAny.FindAllStringSubmatch(out, -1); len(ms) > 0 {
		return ms[len(ms)-1][1]
	}
	return ""
}

func normalizeTo4(s string) string {
	res := make([]rune, 0, 4)
	for _, r := range s {
		if len(res) >= 4 {
			break
		}
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			if r >= 'a' && r <= 'z' {
				r = r - 'a' + 'A'
			}
			res = append(res, r)
		}
	}
	return string(res)
}
