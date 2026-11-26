package domain

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/claviq/desk/backend/internal/models"
)

type CustomerInput struct {
	Name     *string `json:"name"`
	Company  *string `json:"company"`
	Phone    *string `json:"phone"`
	Email    *string `json:"email"`
	Address  *string `json:"address"`
	TaxRegNo *string `json:"tax_reg_no"`
	Notes    *string `json:"notes"`
}

func (in CustomerInput) apply(c *models.Customer) error {
	if in.Name != nil {
		c.Name = strings.TrimSpace(*in.Name)
	}
	if c.Name == "" {
		return invalid("customer name is required")
	}
	set := func(dst **string, v *string) {
		if v != nil {
			*dst = trimPtr(v)
		}
	}
	set(&c.Company, in.Company)
	set(&c.Phone, in.Phone)
	set(&c.Email, in.Email)
	set(&c.Address, in.Address)
	set(&c.TaxRegNo, in.TaxRegNo)
	set(&c.Notes, in.Notes)
	if c.Email != nil && !strings.Contains(*c.Email, "@") {
		return invalid("customer email %q is not valid", *c.Email)
	}
	return nil
}

func (s *Service) CreateCustomer(ctx context.Context, a Actor, in CustomerInput) (*models.Customer, error) {
	c := models.Customer{TenantID: a.TenantID}
	if err := in.apply(&c); err != nil {
		return nil, err
	}
	err := s.tx(ctx, a, func(tx *gorm.DB) error {
		if err := tx.Create(&c).Error; err != nil {
			return err
		}
		return s.audit(tx, a, "create", "customer", c.ID, c)
	})
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Service) UpdateCustomer(ctx context.Context, a Actor, id uuid.UUID, in CustomerInput) (*models.Customer, error) {
	var c models.Customer
	err := s.tx(ctx, a, func(tx *gorm.DB) error {
		if err := tx.First(&c, "id = ?", id).Error; err != nil {
			return err
		}
		before := c
		if err := in.apply(&c); err != nil {
			return err
		}
		if err := tx.Save(&c).Error; err != nil {
			return err
		}
		return s.audit(tx, a, "update", "customer", c.ID, map[string]any{"before": before, "after": c})
	})
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Service) SetCustomerArchived(ctx context.Context, a Actor, id uuid.UUID, archived bool) (*models.Customer, error) {
	var c models.Customer
	err := s.tx(ctx, a, func(tx *gorm.DB) error {
		if err := tx.First(&c, "id = ?", id).Error; err != nil {
			return err
		}
		if archived {
			now := time.Now()
			c.ArchivedAt = &now
		} else {
			c.ArchivedAt = nil
		}
		if err := tx.Save(&c).Error; err != nil {
			return err
		}
		action := "unarchive"
		if archived {
			action = "archive"
		}
		return s.audit(tx, a, action, "customer", c.ID, nil)
	})
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Service) GetCustomer(ctx context.Context, a Actor, id uuid.UUID) (*models.Customer, error) {
	var c models.Customer
	if err := s.tx(ctx, a, func(tx *gorm.DB) error { return tx.First(&c, "id = ?", id).Error }); err != nil {
		return nil, err
	}
	return &c, nil
}

type CustomerFilter struct {
	Query           string
	IncludeArchived bool
	Limit, Offset   int
}

const customerSearchExpr = "lower(name || ' ' || coalesce(company,'') || ' ' || coalesce(email::text,'') || ' ' || coalesce(phone,''))"

// ListCustomers searches name, company, email and phone (substring + fuzzy).
func (s *Service) ListCustomers(ctx context.Context, a Actor, f CustomerFilter) ([]models.Customer, error) {
	if f.Limit <= 0 || f.Limit > 100 {
		f.Limit = 50
	}
	var out []models.Customer
	err := s.tx(ctx, a, func(tx *gorm.DB) error {
		q := tx.Limit(f.Limit).Offset(f.Offset)
		if !f.IncludeArchived {
			q = q.Where("archived_at IS NULL")
		}
		if qs := strings.ToLower(strings.TrimSpace(f.Query)); qs != "" {
			like := "%" + escapeLike(qs) + "%"
			q = q.Where(customerSearchExpr+" LIKE ? OR word_similarity(?, "+customerSearchExpr+") > 0.4", like, qs).
				Order(gorm.Expr("word_similarity(?, "+customerSearchExpr+") DESC", qs))
		}
		return q.Order("name").Find(&out).Error
	})
	return out, err
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
