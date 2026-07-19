package domain

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

type Product struct {
	ID                      string    `json:"id" yaml:"id"`
	Name                    string    `json:"name" yaml:"name"`
	Repository              string    `json:"repository,omitempty" yaml:"repository,omitempty"`
	RepositoryID            int64     `json:"repository_id,omitempty" yaml:"repository_id,omitempty"`
	LocalRepository         string    `json:"local_repository,omitempty" yaml:"local_repository,omitempty"`
	Website                 string    `json:"website,omitempty" yaml:"website,omitempty"`
	DocumentationURL        string    `json:"documentation_url,omitempty" yaml:"documentation_url,omitempty"`
	PricingURL              string    `json:"pricing_url,omitempty" yaml:"pricing_url,omitempty"`
	ChangelogURL            string    `json:"changelog_url,omitempty" yaml:"changelog_url,omitempty"`
	ProductType             string    `json:"product_type" yaml:"product_type"`
	PrimaryConversionAction string    `json:"primary_conversion_action" yaml:"primary_conversion_action"`
	DefaultLanguage         string    `json:"default_language" yaml:"default_language"`
	CreatedAt               time.Time `json:"created_at" yaml:"-"`
	UpdatedAt               time.Time `json:"updated_at" yaml:"-"`
}

func ValidateProductID(id string) error {
	if !slugPattern.MatchString(id) || len(id) > 64 {
		return errors.New("product id must be a lowercase kebab-case slug of at most 64 characters")
	}
	return nil
}

func (p Product) Validate() error {
	if err := ValidateProductID(p.ID); err != nil {
		return err
	}
	if strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.ProductType) == "" || strings.TrimSpace(p.PrimaryConversionAction) == "" || strings.TrimSpace(p.DefaultLanguage) == "" {
		return errors.New("product name, product_type, primary_conversion_action, and default_language are required")
	}
	if p.Repository != "" {
		parts := strings.Split(p.Repository, "/")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return errors.New("repository must use owner/name format")
		}
	}
	return nil
}

type ContextStatus string

const (
	ContextDraft      ContextStatus = "draft"
	ContextApproved   ContextStatus = "approved"
	ContextSuperseded ContextStatus = "superseded"
)

type ContextVersion struct {
	ID          string        `json:"id"`
	ProductID   string        `json:"product_id"`
	Version     int           `json:"version"`
	Status      ContextStatus `json:"status"`
	Content     string        `json:"content"`
	ContentHash string        `json:"content_hash"`
	EvidenceIDs []string      `json:"evidence_ids"`
	Uncertainty []string      `json:"unsupported_or_uncertain"`
	CreatedAt   time.Time     `json:"created_at"`
	ApprovedAt  *time.Time    `json:"approved_at,omitempty"`
	ApprovedBy  string        `json:"approved_by,omitempty"`
}
