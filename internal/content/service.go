package content

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"strings"
)

var (
	ErrRevisionConflict = errors.New("content revision conflict")
	ErrSlugConflict     = errors.New("content slug conflict")
)

// Service provides content mutations without depending on HTTP or legacy handlers.
type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) Create(input SaveInput) (*Item, string, error) {
	if err := s.checkSlug(input.Slug, input.Type, ""); err != nil {
		return nil, "", err
	}
	item, err := s.repo.Save(input)
	if err != nil {
		return nil, "", err
	}
	revision, err := Revision(item.Path)
	return item, revision, err
}

func (s *Service) Update(input SaveInput, expectedRevision string) (*Item, string, error) {
	current, err := s.repo.GetByID(input.ID, input.Type)
	if err != nil {
		return nil, "", err
	}
	if err := matchRevision(current.Path, expectedRevision); err != nil {
		return nil, "", err
	}
	if err := s.checkSlug(input.Slug, input.Type, input.ID); err != nil {
		return nil, "", err
	}
	item, err := s.repo.Save(input)
	if err != nil {
		return nil, "", err
	}
	revision, err := Revision(item.Path)
	return item, revision, err
}

func (s *Service) Delete(id string, typ Type, expectedRevision string) error {
	item, err := s.repo.GetByID(id, typ)
	if err != nil {
		return err
	}
	if err := matchRevision(item.Path, expectedRevision); err != nil {
		return err
	}
	return s.repo.Delete(item.ID, typ)
}

func Revision(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func matchRevision(path, expected string) error {
	expected = strings.Trim(strings.TrimSpace(expected), `"`)
	if expected == "" {
		return nil
	}
	actual, err := Revision(path)
	if err != nil {
		return err
	}
	if expected != actual {
		return ErrRevisionConflict
	}
	return nil
}

func (s *Service) checkSlug(slug string, typ Type, exceptID string) error {
	if typ == TypeNote || strings.TrimSpace(slug) == "" {
		return nil
	}
	items, err := s.repo.List(typ)
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.ID != exceptID && item.Slug == slug {
			return ErrSlugConflict
		}
	}
	return nil
}
