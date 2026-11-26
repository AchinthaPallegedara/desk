package domain

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"path"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/claviq/desk/backend/internal/models"
)

type Settings struct {
	Tenant       models.Tenant           `json:"tenant"`
	HasLogo      bool                    `json:"has_logo"`
	TaxRates     []models.TaxRate        `json:"tax_rates"`
	BankAccounts []models.BankAccount    `json:"bank_accounts"`
	Sequences    []models.NumberSequence `json:"number_sequences"`
}

func (s *Service) GetSettings(ctx context.Context, a Actor) (*Settings, error) {
	var out Settings
	err := s.tx(ctx, a, func(tx *gorm.DB) error {
		if err := tx.First(&out.Tenant, "id = ?", a.TenantID).Error; err != nil {
			return err
		}
		out.HasLogo = out.Tenant.LogoPath != nil
		if err := tx.Order("is_default DESC, name").Find(&out.TaxRates).Error; err != nil {
			return err
		}
		if err := tx.Order("is_default DESC, created_at").Find(&out.BankAccounts).Error; err != nil {
			return err
		}
		return tx.Order("doc_type").Find(&out.Sequences).Error
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

type TenantInput struct {
	Name                *string `json:"name"`
	LegalName           *string `json:"legal_name"`
	RegNo               *string `json:"reg_no"`
	TaxRegNo            *string `json:"tax_reg_no"`
	Address             *string `json:"address"`
	Phone               *string `json:"phone"`
	Email               *string `json:"email"`
	Currency            *string `json:"currency"`
	DefaultTerms        *string `json:"default_terms"`
	DefaultValidityDays *int    `json:"default_validity_days"`
	DefaultPaymentDays  *int    `json:"default_payment_days"`
}

func (s *Service) UpdateTenant(ctx context.Context, a Actor, in TenantInput) (*models.Tenant, error) {
	upd := map[string]any{}
	if in.Name != nil {
		n := strings.TrimSpace(*in.Name)
		if n == "" {
			return nil, invalid("business name is required")
		}
		upd["name"] = n
	}
	setOpt := func(col string, v *string) {
		if v != nil {
			upd[col] = trimPtr(v)
		}
	}
	setOpt("legal_name", in.LegalName)
	setOpt("reg_no", in.RegNo)
	setOpt("tax_reg_no", in.TaxRegNo)
	setOpt("address", in.Address)
	setOpt("phone", in.Phone)
	setOpt("email", in.Email)
	setOpt("default_terms", in.DefaultTerms)
	if in.Currency != nil {
		c := strings.ToUpper(strings.TrimSpace(*in.Currency))
		if len(c) != 3 {
			return nil, invalid("currency must be a 3-letter ISO code, e.g. LKR")
		}
		upd["currency"] = c
	}
	if in.DefaultValidityDays != nil {
		upd["default_validity_days"] = *in.DefaultValidityDays
	}
	if in.DefaultPaymentDays != nil {
		upd["default_payment_days"] = *in.DefaultPaymentDays
	}

	var t models.Tenant
	err := s.tx(ctx, a, func(tx *gorm.DB) error {
		if len(upd) > 0 {
			if err := tx.Model(&models.Tenant{}).Where("id = ?", a.TenantID).Updates(upd).Error; err != nil {
				return err
			}
			if err := s.audit(tx, a, "update", "tenant", a.TenantID, in); err != nil {
				return err
			}
		}
		return tx.First(&t, "id = ?", a.TenantID).Error
	})
	if err != nil {
		return nil, err
	}
	return &t, nil
}

const maxLogoBytes = 2 << 20

// UploadLogo stores a PNG, JPEG or SVG logo for use in templates.
func (s *Service) UploadLogo(ctx context.Context, a Actor, data []byte) error {
	if len(data) == 0 || len(data) > maxLogoBytes {
		return invalid("logo must be between 1 byte and 2 MB")
	}
	ext := ""
	switch ct := http.DetectContentType(data); {
	case ct == "image/png":
		ext = "png"
	case ct == "image/jpeg":
		ext = "jpg"
	case bytes.Contains(data[:min(len(data), 512)], []byte("<svg")):
		ext = "svg"
	default:
		return invalid("logo must be PNG, JPEG or SVG")
	}
	key := fmt.Sprintf("tenants/%s/logo-%s.%s", a.TenantID, uuid.Must(uuid.NewV7()).String()[:8], ext)
	if err := s.store.Put(ctx, key, data, "image/"+map[string]string{"png": "png", "jpg": "jpeg", "svg": "svg+xml"}[ext]); err != nil {
		return err
	}
	return s.tx(ctx, a, func(tx *gorm.DB) error {
		if err := tx.Model(&models.Tenant{}).Where("id = ?", a.TenantID).Update("logo_path", key).Error; err != nil {
			return err
		}
		return s.audit(tx, a, "upload_logo", "tenant", a.TenantID, map[string]string{"logo_path": key})
	})
}

func (s *Service) RemoveLogo(ctx context.Context, a Actor) error {
	return s.tx(ctx, a, func(tx *gorm.DB) error {
		if err := tx.Model(&models.Tenant{}).Where("id = ?", a.TenantID).Update("logo_path", nil).Error; err != nil {
			return err
		}
		return s.audit(tx, a, "remove_logo", "tenant", a.TenantID, nil)
	})
}

// GetLogo returns the logo bytes and its file extension ("" when none).
func (s *Service) GetLogo(ctx context.Context, logoPath *string) ([]byte, string, error) {
	if logoPath == nil || *logoPath == "" {
		return nil, "", nil
	}
	b, err := s.store.Get(ctx, *logoPath)
	if err != nil {
		return nil, "", err
	}
	return b, strings.TrimPrefix(path.Ext(*logoPath), "."), nil
}

// ---------- tax rates ----------

type TaxRateInput struct {
	Name      *string          `json:"name"`
	Rate      *decimal.Decimal `json:"rate"`
	IsDefault *bool            `json:"is_default"`
	Active    *bool            `json:"active"`
}

func (s *Service) SaveTaxRate(ctx context.Context, a Actor, id *uuid.UUID, in TaxRateInput) (*models.TaxRate, error) {
	var tr models.TaxRate
	err := s.tx(ctx, a, func(tx *gorm.DB) error {
		if id != nil {
			if err := tx.First(&tr, "id = ?", *id).Error; err != nil {
				return err
			}
		} else {
			tr = models.TaxRate{TenantID: a.TenantID, Active: true}
			if in.Name == nil || in.Rate == nil {
				return invalid("tax rate name and rate are required")
			}
		}
		if in.Name != nil {
			tr.Name = strings.TrimSpace(*in.Name)
			if tr.Name == "" {
				return invalid("tax rate name is required")
			}
		}
		if in.Rate != nil {
			if in.Rate.IsNegative() || in.Rate.GreaterThan(decimal.NewFromInt(100)) {
				return invalid("tax rate must be between 0 and 100")
			}
			tr.Rate = in.Rate.Round(3)
		}
		if in.Active != nil {
			tr.Active = *in.Active
		}
		if in.IsDefault != nil {
			if *in.IsDefault && !tr.Active {
				return invalid("an inactive tax rate cannot be the default")
			}
			if *in.IsDefault {
				if err := tx.Model(&models.TaxRate{}).Where("is_default AND id <> ?", tr.ID).Update("is_default", false).Error; err != nil {
					return err
				}
			}
			tr.IsDefault = *in.IsDefault
		}
		if !tr.Active {
			tr.IsDefault = false
		}
		if err := tx.Save(&tr).Error; err != nil {
			return err
		}
		return s.audit(tx, a, "save", "tax_rate", tr.ID, in)
	})
	if err != nil {
		return nil, err
	}
	return &tr, nil
}

// ---------- bank accounts ----------

type BankAccountInput struct {
	AccountName   *string `json:"account_name"`
	AccountNumber *string `json:"account_number"`
	Bank          *string `json:"bank"`
	Branch        *string `json:"branch"`
	Swift         *string `json:"swift"`
	IsDefault     *bool   `json:"is_default"`
	Active        *bool   `json:"active"`
}

func (s *Service) SaveBankAccount(ctx context.Context, a Actor, id *uuid.UUID, in BankAccountInput) (*models.BankAccount, error) {
	var b models.BankAccount
	err := s.tx(ctx, a, func(tx *gorm.DB) error {
		if id != nil {
			if err := tx.First(&b, "id = ?", *id).Error; err != nil {
				return err
			}
		} else {
			b = models.BankAccount{TenantID: a.TenantID, Active: true}
		}
		req := func(dst *string, v *string, label string) error {
			if v != nil {
				*dst = strings.TrimSpace(*v)
			}
			if *dst == "" {
				return invalid("%s is required", label)
			}
			return nil
		}
		if err := req(&b.AccountName, in.AccountName, "account name"); err != nil {
			return err
		}
		if err := req(&b.AccountNumber, in.AccountNumber, "account number"); err != nil {
			return err
		}
		if err := req(&b.Bank, in.Bank, "bank"); err != nil {
			return err
		}
		if in.Branch != nil {
			b.Branch = trimPtr(in.Branch)
		}
		if in.Swift != nil {
			b.Swift = trimPtr(in.Swift)
		}
		if in.Active != nil {
			b.Active = *in.Active
		}
		if in.IsDefault != nil {
			if *in.IsDefault {
				if err := tx.Model(&models.BankAccount{}).Where("is_default AND id <> ?", b.ID).Update("is_default", false).Error; err != nil {
					return err
				}
			}
			b.IsDefault = *in.IsDefault
		}
		if !b.Active {
			b.IsDefault = false
		}
		if err := tx.Save(&b).Error; err != nil {
			return err
		}
		return s.audit(tx, a, "save", "bank_account", b.ID, in)
	})
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// ---------- number sequences ----------

type SequenceInput struct {
	Prefix      *string `json:"prefix"`
	Format      *string `json:"format"`
	ResetPolicy *string `json:"reset_policy"`
	NextValue   *int    `json:"next_value"`
}

func (s *Service) UpdateSequence(ctx context.Context, a Actor, docType string, in SequenceInput) (*models.NumberSequence, error) {
	if docType != "estimate" && docType != "invoice" {
		return nil, invalid("doc_type must be estimate or invoice")
	}
	var seq models.NumberSequence
	err := s.tx(ctx, a, func(tx *gorm.DB) error {
		if err := lockSequence(tx, docType, &seq); err != nil {
			return err
		}
		if in.Prefix != nil {
			seq.Prefix = strings.TrimSpace(*in.Prefix)
		}
		if in.Format != nil {
			f := strings.TrimSpace(*in.Format)
			if !strings.Contains(f, "{seq") {
				return invalid("number format must contain {seq} or {seq:N}")
			}
			seq.Format = f
		}
		if in.ResetPolicy != nil {
			if *in.ResetPolicy != "never" && *in.ResetPolicy != "yearly" {
				return invalid("reset_policy must be never or yearly")
			}
			seq.ResetPolicy = *in.ResetPolicy
		}
		if in.NextValue != nil {
			if *in.NextValue < 1 {
				return invalid("next_value must be at least 1")
			}
			seq.NextValue = *in.NextValue
		}
		if err := tx.Save(&seq).Error; err != nil {
			return err
		}
		return s.audit(tx, a, "update", "number_sequence", docType, in)
	})
	if err != nil {
		return nil, err
	}
	return &seq, nil
}
