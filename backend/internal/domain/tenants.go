package domain

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/claviq/desk/backend/assets"
	"github.com/claviq/desk/backend/internal/models"
)

type NewTenant struct {
	BusinessName string
	OwnerEmail   string
	PasswordHash string
}

// BootstrapTenant creates a tenant with its owner user, number sequences,
// default VAT rate and default templates. It must be given the schema-owner
// connection: the app role cannot create tenants.
func BootstrapTenant(ctx context.Context, ownerDB *gorm.DB, in NewTenant) (*models.Tenant, *models.User, error) {
	name := strings.TrimSpace(in.BusinessName)
	email := strings.ToLower(strings.TrimSpace(in.OwnerEmail))
	if name == "" || !strings.Contains(email, "@") || in.PasswordHash == "" {
		return nil, nil, invalid("business name, owner email and password are required")
	}
	var tenant models.Tenant
	var user models.User
	err := ownerDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		terms := "1. This document is valid for 30 days from the date of issue.\n2. Payments are due as per the schedule above.\n3. Prices are in LKR and include taxes where shown."
		tenant = models.Tenant{ID: uuid.Must(uuid.NewV7()), Name: name, Currency: "LKR", DefaultTerms: &terms,
			DefaultValidityDays: 30, DefaultPaymentDays: 14}
		if err := tx.Create(&tenant).Error; err != nil {
			return err
		}
		user = models.User{TenantID: tenant.ID, Email: email, PasswordHash: in.PasswordHash, Role: "owner"}
		if err := tx.Create(&user).Error; err != nil {
			return err
		}
		for _, seq := range []models.NumberSequence{
			{TenantID: tenant.ID, DocType: "estimate", Prefix: "EST", Format: "{prefix}-{yyyy}-{seq:4}", ResetPolicy: "yearly", NextValue: 1},
			{TenantID: tenant.ID, DocType: "invoice", Prefix: "INV", Format: "{prefix}-{yyyy}-{seq:4}", ResetPolicy: "yearly", NextValue: 1},
		} {
			if err := tx.Create(&seq).Error; err != nil {
				return err
			}
		}
		if err := tx.Create(&models.TaxRate{TenantID: tenant.ID, Name: "VAT", Rate: decimal.NewFromInt(18), IsDefault: true, Active: true}).Error; err != nil {
			return err
		}
		for _, dt := range []string{KindEstimate, KindInvoice} {
			t := models.Template{TenantID: tenant.ID, Name: "Standard " + dt, DocType: dt, IsDefault: true}
			if err := tx.Create(&t).Error; err != nil {
				return err
			}
			tv := models.TemplateVersion{TenantID: tenant.ID, TemplateID: t.ID, DocType: dt, Version: 1,
				TypstSource: assets.DefaultTemplate(dt), CreatedVia: "system"}
			if err := tx.Create(&tv).Error; err != nil {
				return err
			}
			if err := tx.Model(&t).Update("current_version_id", tv.ID).Error; err != nil {
				return err
			}
		}
		return tx.Create(&models.AuditLog{TenantID: tenant.ID, UserID: &user.ID, Via: "system",
			Action: "create", Entity: "tenant", EntityID: ptr(tenant.ID.String())}).Error
	})
	return &tenant, &user, mapError(err)
}

func ptr[T any](v T) *T { return &v }
