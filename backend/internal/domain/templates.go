package domain

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/claviq/desk/backend/assets"
	"github.com/claviq/desk/backend/internal/models"
	"github.com/claviq/desk/backend/internal/render"
)

type TemplateView struct {
	ID             uuid.UUID  `json:"id"`
	Name           string     `json:"name"`
	DocType        string     `json:"doc_type"`
	IsDefault      bool       `json:"is_default"`
	CurrentVersion int        `json:"current_version"`
	ArchivedAt     *time.Time `json:"archived_at,omitempty"`
	UpdatedAt      time.Time  `json:"updated_at"`
	Source         string     `json:"typst_source,omitempty"`
}

type TemplateInput struct {
	Name      *string `json:"name"`
	DocType   string  `json:"doc_type"` // create only
	Source    *string `json:"typst_source"`
	IsDefault *bool   `json:"is_default"`
}

func (s *Service) templateView(tx *gorm.DB, t *models.Template, withSource bool) (*TemplateView, error) {
	v := &TemplateView{ID: t.ID, Name: t.Name, DocType: t.DocType, IsDefault: t.IsDefault,
		ArchivedAt: t.ArchivedAt, UpdatedAt: t.UpdatedAt}
	if t.CurrentVersionID != nil {
		var tv models.TemplateVersion
		q := tx
		if !withSource {
			q = q.Select("id, version")
		}
		if err := q.First(&tv, "id = ?", *t.CurrentVersionID).Error; err != nil {
			return nil, err
		}
		v.CurrentVersion, v.Source = tv.Version, tv.TypstSource
	}
	return v, nil
}

func (s *Service) ListTemplates(ctx context.Context, a Actor, docType string, includeArchived bool) ([]TemplateView, error) {
	var out []TemplateView
	err := s.tx(ctx, a, func(tx *gorm.DB) error {
		var ts []models.Template
		q := tx.Order("doc_type, is_default DESC, name")
		if docType != "" {
			q = q.Where("doc_type = ?", docType)
		}
		if !includeArchived {
			q = q.Where("archived_at IS NULL")
		}
		if err := q.Find(&ts).Error; err != nil {
			return err
		}
		for i := range ts {
			v, err := s.templateView(tx, &ts[i], false)
			if err != nil {
				return err
			}
			out = append(out, *v)
		}
		return nil
	})
	if out == nil {
		out = []TemplateView{}
	}
	return out, err
}

func (s *Service) GetTemplate(ctx context.Context, a Actor, id uuid.UUID) (*TemplateView, error) {
	var v *TemplateView
	err := s.tx(ctx, a, func(tx *gorm.DB) error {
		var t models.Template
		if err := tx.First(&t, "id = ?", id).Error; err != nil {
			return err
		}
		var err error
		v, err = s.templateView(tx, &t, true)
		return err
	})
	return v, err
}

// validateTemplate compiles the source against sample data so broken templates
// are rejected with Typst's error message (which the AI can act on).
func (s *Service) validateTemplate(ctx context.Context, tx *gorm.DB, tenantID uuid.UUID, docType, src string) error {
	dirs, err := s.fontDirs(ctx, tx, tenantID)
	if err != nil {
		return err
	}
	_, err = s.renderer.Render(ctx, render.Job{Source: src, Data: SampleData(docType), FontDirs: dirs, Format: render.PDF})
	var ce *render.CompileError
	if errors.As(err, &ce) {
		return invalid("template does not compile: %s", ce.Message)
	}
	return err
}

func (s *Service) addVersion(tx *gorm.DB, a Actor, t *models.Template, src string) (*models.TemplateVersion, error) {
	var maxV int
	if err := tx.Model(&models.TemplateVersion{}).Where("template_id = ?", t.ID).
		Select("coalesce(max(version), 0)").Scan(&maxV).Error; err != nil {
		return nil, err
	}
	via := a.Via
	if via == "" {
		via = "system"
	}
	tv := &models.TemplateVersion{TenantID: t.TenantID, TemplateID: t.ID, DocType: t.DocType,
		Version: maxV + 1, TypstSource: src, CreatedByUserID: a.UserID, CreatedVia: via}
	if err := tx.Create(tv).Error; err != nil {
		return nil, err
	}
	t.CurrentVersionID = &tv.ID
	return tv, tx.Model(t).Update("current_version_id", tv.ID).Error
}

// CreateTemplate creates a template; with no source it starts from the built-in default.
func (s *Service) CreateTemplate(ctx context.Context, a Actor, in TemplateInput) (*TemplateView, error) {
	if in.DocType != KindEstimate && in.DocType != KindInvoice {
		return nil, invalid("doc_type must be estimate or invoice")
	}
	if in.Name == nil || strings.TrimSpace(*in.Name) == "" {
		return nil, invalid("template name is required")
	}
	src := assets.DefaultTemplate(in.DocType)
	if in.Source != nil && strings.TrimSpace(*in.Source) != "" {
		src = *in.Source
	}
	var v *TemplateView
	err := s.tx(ctx, a, func(tx *gorm.DB) error {
		if err := s.validateTemplate(ctx, tx, a.TenantID, in.DocType, src); err != nil {
			return err
		}
		t := models.Template{TenantID: a.TenantID, Name: strings.TrimSpace(*in.Name), DocType: in.DocType}
		if in.IsDefault != nil && *in.IsDefault {
			if err := tx.Model(&models.Template{}).Where("doc_type = ? AND is_default", in.DocType).
				Update("is_default", false).Error; err != nil {
				return err
			}
			t.IsDefault = true
		}
		if err := tx.Create(&t).Error; err != nil {
			return err
		}
		if _, err := s.addVersion(tx, a, &t, src); err != nil {
			return err
		}
		if err := s.audit(tx, a, "create", "template", t.ID, map[string]any{"name": t.Name, "doc_type": t.DocType}); err != nil {
			return err
		}
		var err error
		v, err = s.templateView(tx, &t, true)
		return err
	})
	return v, err
}

// UpdateTemplate renames, sets default, and/or saves a new source version.
func (s *Service) UpdateTemplate(ctx context.Context, a Actor, id uuid.UUID, in TemplateInput) (*TemplateView, error) {
	var v *TemplateView
	err := s.tx(ctx, a, func(tx *gorm.DB) error {
		var t models.Template
		if err := tx.First(&t, "id = ?", id).Error; err != nil {
			return err
		}
		if t.ArchivedAt != nil {
			return invalid("template is archived")
		}
		changes := map[string]any{}
		if in.Name != nil {
			n := strings.TrimSpace(*in.Name)
			if n == "" {
				return invalid("template name is required")
			}
			if err := tx.Model(&t).Update("name", n).Error; err != nil {
				return err
			}
			changes["name"] = n
		}
		if in.IsDefault != nil {
			if *in.IsDefault {
				if err := tx.Model(&models.Template{}).Where("doc_type = ? AND is_default AND id <> ?", t.DocType, t.ID).
					Update("is_default", false).Error; err != nil {
					return err
				}
			}
			if err := tx.Model(&t).Update("is_default", *in.IsDefault).Error; err != nil {
				return err
			}
			changes["is_default"] = *in.IsDefault
		}
		if in.Source != nil {
			if err := s.validateTemplate(ctx, tx, a.TenantID, t.DocType, *in.Source); err != nil {
				return err
			}
			tv, err := s.addVersion(tx, a, &t, *in.Source)
			if err != nil {
				return err
			}
			changes["version"] = tv.Version
		}
		if err := s.audit(tx, a, "update", "template", t.ID, changes); err != nil {
			return err
		}
		var err error
		v, err = s.templateView(tx, &t, true)
		return err
	})
	return v, err
}

func (s *Service) SetTemplateArchived(ctx context.Context, a Actor, id uuid.UUID, archived bool) error {
	return s.tx(ctx, a, func(tx *gorm.DB) error {
		var t models.Template
		if err := tx.First(&t, "id = ?", id).Error; err != nil {
			return err
		}
		if archived && t.IsDefault {
			return invalid("make another template the default before archiving this one")
		}
		var at *time.Time
		if archived {
			now := time.Now()
			at = &now
		}
		if err := tx.Model(&t).Update("archived_at", at).Error; err != nil {
			return err
		}
		return s.audit(tx, a, map[bool]string{true: "archive", false: "unarchive"}[archived], "template", id, nil)
	})
}

func (s *Service) ListTemplateVersions(ctx context.Context, a Actor, id uuid.UUID) ([]models.TemplateVersion, error) {
	var out []models.TemplateVersion
	err := s.tx(ctx, a, func(tx *gorm.DB) error {
		return tx.Select("id, template_id, doc_type, version, created_by_user_id, created_via, created_at").
			Where("template_id = ?", id).Order("version DESC").Find(&out).Error
	})
	return out, err
}

func (s *Service) GetTemplateVersion(ctx context.Context, a Actor, versionID uuid.UUID) (*models.TemplateVersion, error) {
	var tv models.TemplateVersion
	if err := s.tx(ctx, a, func(tx *gorm.DB) error { return tx.First(&tv, "id = ?", versionID).Error }); err != nil {
		return nil, err
	}
	return &tv, nil
}

// RestoreTemplateVersion copies an old version into a new current version.
func (s *Service) RestoreTemplateVersion(ctx context.Context, a Actor, templateID, versionID uuid.UUID) (*TemplateView, error) {
	var v *TemplateView
	err := s.tx(ctx, a, func(tx *gorm.DB) error {
		var t models.Template
		if err := tx.First(&t, "id = ?", templateID).Error; err != nil {
			return err
		}
		var old models.TemplateVersion
		if err := tx.First(&old, "id = ? AND template_id = ?", versionID, templateID).Error; err != nil {
			return err
		}
		tv, err := s.addVersion(tx, a, &t, old.TypstSource)
		if err != nil {
			return err
		}
		if err := s.audit(tx, a, "restore_version", "template", t.ID, map[string]any{"from_version": old.Version, "new_version": tv.Version}); err != nil {
			return err
		}
		v, err = s.templateView(tx, &t, true)
		return err
	})
	return v, err
}

// TemplatePreviewLinks renders the template's current (or given) version with sample data.
func (s *Service) TemplatePreviewLinks(ctx context.Context, a Actor, templateID uuid.UUID, versionID *uuid.UUID) (*Links, error) {
	var vid uuid.UUID
	err := s.tx(ctx, a, func(tx *gorm.DB) error {
		var t models.Template
		if err := tx.First(&t, "id = ?", templateID).Error; err != nil {
			return err
		}
		if versionID != nil {
			var tv models.TemplateVersion
			if err := tx.Select("id").First(&tv, "id = ? AND template_id = ?", *versionID, templateID).Error; err != nil {
				return err
			}
			vid = tv.ID
		} else if t.CurrentVersionID != nil {
			vid = *t.CurrentVersionID
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.makeLinks("t", vid, a.TenantID)
}

// SampleData is realistic data for template previews and validation.
func SampleData(docType string) RenderData {
	d := RenderData{
		Doc: RenderDoc{Kind: docType, Title: "Estimate", Subject: "Website redesign and booking system",
			Number: "EST-2026-0001", IsDraft: false, IssueDate: "26 Nov 2026", ValidUntil: "26 Dec 2026",
			Currency: "LKR", Notes: "Scope covers design, development and two rounds of revisions.",
			Terms: "1. This document is valid for 30 days from the date of issue.\n2. Work begins on receipt of the first payment.\n3. Prices exclude third-party licence and hosting fees unless stated."},
		Issuer: RenderIssuer{Name: "Your Business (Pvt) Ltd", RegNo: "PV 000000", TaxRegNo: "000000000-7000",
			Address: "No. 1, Main Street, Colombo 03", Phone: "+94 77 000 0000", Email: "hello@example.com"},
		Customer: RenderCustomer{Name: "Nimal Perera", Company: "Perera Holdings", Address: "45 Galle Road, Colombo 03",
			Phone: "+94 71 555 0101", Email: "nimal@example.lk"},
		Items: []RenderItem{
			{No: 1, Description: "UX research and wireframes", Quantity: "1", UnitPrice: "150,000.00", Tax: "VAT 18%", Amount: "150,000.00"},
			{No: 2, Description: "Visual design — 8 page templates", Quantity: "8", UnitPrice: "25,000.00", Discount: "10%", Tax: "VAT 18%", Amount: "180,000.00"},
			{No: 3, Description: "Development and CMS integration", Quantity: "120", UnitPrice: "4,500.00", Tax: "VAT 18%", Amount: "540,000.00"},
		},
		Addons:      []RenderItem{{No: 1, Description: "Annual maintenance package", Quantity: "1", UnitPrice: "120,000.00", Tax: "VAT 18%", Amount: "120,000.00"}},
		AddonsTotal: "141,600.00",
		Totals: RenderTotals{Subtotal: "890,000.00", Discount: "20,000.00", Tax: "156,600.00", Total: "1,026,600.00",
			Taxes: []RenderTax{{Name: "VAT", Rate: "18", Amount: "156,600.00"}}},
		Milestones: []RenderMilestone{
			{Name: "Kick-off", Note: "On signing", Percent: "30", Amount: "307,980.00", DueDate: "01 Dec 2026"},
			{Name: "Design approval", Percent: "20", Amount: "205,320.00", DueDate: "15 Dec 2026"},
			{Name: "Beta release", Percent: "20", Amount: "205,320.00", DueDate: "15 Jan 2027"},
			{Name: "Launch", Percent: "20", Amount: "205,320.00", DueDate: "01 Feb 2027"},
			{Name: "Post-launch support", Note: "30 days after launch", Percent: "10", Amount: "102,660.00"},
		},
		Bank: &RenderBank{AccountName: "Your Business (Pvt) Ltd", AccountNumber: "0000 0000 0000", Bank: "Commercial Bank",
			Branch: "Colombo", Swift: "CCEYLKLX"},
	}
	if docType == KindInvoice {
		d.Doc.Title, d.Doc.Number, d.Doc.ValidUntil = "Invoice", "INV-2026-0001", ""
		d.Doc.DueDate, d.Doc.EstimateNumber = "10 Dec 2026", "EST-2026-0001"
		d.Addons, d.AddonsTotal, d.Milestones = []RenderItem{}, "", []RenderMilestone{}
	}
	return d
}
