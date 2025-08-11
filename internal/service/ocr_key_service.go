package service

import (
	"errors"
	"wjdr-backend-go/internal/model"
	"wjdr-backend-go/internal/repository"

	"go.uber.org/zap"
)

// OCRKeyService 业务封装
type OCRKeyService struct {
	repo   *repository.OCRKeyRepository
	logger *zap.Logger
}

func NewOCRKeyService(repo *repository.OCRKeyRepository, logger *zap.Logger) *OCRKeyService {
	return &OCRKeyService{repo: repo, logger: logger}
}

func (s *OCRKeyService) ListAll() ([]model.OCRKey, error) {
	return s.repo.ListAll()
}

func (s *OCRKeyService) ListUsable() ([]model.OCRKey, error) {
	return s.repo.ListUsable()
}

func (s *OCRKeyService) Create(k *model.OCRKey) (int, error) {
	if k.Provider == "" {
		k.Provider = "baidu"
	}
	if k.Weight <= 0 {
		k.Weight = 1
	}
	return s.repo.Create(*k)
}

func (s *OCRKeyService) Update(id int, patch map[string]interface{}) error {
	if len(patch) == 0 {
		return errors.New("empty update patch")
	}
	return s.repo.Update(id, patch)
}

func (s *OCRKeyService) Delete(id int) error {
	return s.repo.Delete(id)
}

func (s *OCRKeyService) MarkQuota(id int, hasQuota bool) error {
	return s.repo.MarkQuota(id, hasQuota)
}

func (s *OCRKeyService) TouchUsage(id int, success bool, errMsg *string) error {
	// 对于 provider=paddle（自研本地模型）不扣减额度；其他正常扣减
	prov, _ := s.repo.GetProviderByID(id)
	if err := s.repo.TouchUsage(id, success, errMsg); err != nil {
		return err
	}
	if prov != "paddle" {
		_ = s.repo.DecrementQuota(id)
	}
	return nil
}

// ResetMonthlyQuota 每月1号执行：将 remaining_quota 重置为 monthly_quota，并启用 has_quota=true（若仍 active）
func (s *OCRKeyService) ResetMonthlyQuota() error {
	return s.repo.ResetMonthlyQuota()
}
