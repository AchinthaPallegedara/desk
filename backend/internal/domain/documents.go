package domain

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/claviq/desk/backend/internal/models"
)

const (
	KindEstimate = "estimate"
	KindInvoice  = "invoice"
)

func checkKind(kind string) error {
	if kind != KindEstimate && kind != KindInvoice {
		return invalid("unknown document kind %q", kind)
	}
	return nil
}

// ---------- inputs ----------

type ItemInput struct {
	Description   string          `json:"description"`
	Quantity      decimal.Decimal `json:"quantity"`
	UnitPrice     decimal.Decimal `json:"unit_price"`
	DiscountType  string          `json:"discount_type"`  // none | percent | amount
	DiscountValue decimal.Decimal `json:"discount_value"` // percent (0–100) or amount
	TaxRateID     *uuid.UUID      `json:"tax_rate_id"`    // nil + !NoTax => tenant default tax
	NoTax         bool            `json:"no_tax"`
	IsAddon       bool            `json:"is_addon"` // estimates only
}

type MilestoneInput struct {
	Name    string          `json:"name"`
	Note    *string         `json:"note"`
	Percent decimal.Decimal `json:"percent"`
	DueDate *models.Date    `json:"due_date"`
}

// DocumentInput creates or patches a draft. Nil fields are left unchanged;
// Items / Milestones, when non-nil, replace the whole list.
type DocumentInput struct {
	CustomerID    *uuid.UUID        `json:"customer_id"`
	TemplateID    *uuid.UUID        `json:"template_id"`
	BankAccountID *uuid.UUID        `json:"bank_account_id"`
	ClearBank     bool              `json:"clear_bank_account"`
	Title         *string           `json:"title"`
	IssueDate     *models.Date      `json:"issue_date"`
	ValidUntil    *models.Date      `json:"valid_until"`
	DueDate       *models.Date      `json:"due_date"`
	Terms         *string           `json:"terms"`
	Notes         *string           `json:"notes"`
	Items         *[]ItemInput      `json:"items"`
	Milestones    *[]MilestoneInput `json:"milestones"`
}

// ---------- views ----------

type CustomerRef struct {
	ID      uuid.UUID `json:"id"`
	Name    string    `json:"name"`
	Company *string   `json:"company,omitempty"`
}

type TemplateRef struct {
	ID      uuid.UUID `json:"id"`
	Name    string    `json:"name"`
	Version int       `json:"version"`
}

type ItemView struct {
	ID             uuid.UUID  `json:"id"`
	Description    string     `json:"description"`
	Quantity       string     `json:"quantity"`
	UnitPrice      string     `json:"unit_price"`
	DiscountType   string     `json:"discount_type"`
	DiscountValue  string     `json:"discount_value"`
	TaxRateID      *uuid.UUID `json:"tax_rate_id"`
	TaxName        string     `json:"tax_name,omitempty"`
	TaxRate        string     `json:"tax_rate,omitempty"`
	IsAddon        bool       `json:"is_addon"`
	Gross          string     `json:"gross"`
	DiscountAmount string     `json:"discount_amount"`
	Net            string     `json:"net"`
	Tax            string     `json:"tax"`
}

type MilestoneView struct {
	ID            uuid.UUID    `json:"id"`
	Name          string       `json:"name"`
	Note          *string      `json:"note,omitempty"`
	Percent       string       `json:"percent"`
	Amount        string       `json:"amount"`
	DueDate       *models.Date `json:"due_date"`
	InvoiceID     *uuid.UUID   `json:"invoice_id"`
	InvoiceNumber *string      `json:"invoice_number,omitempty"`
}

type TaxView struct {
	Name   string `json:"name"`
	Rate   string `json:"rate"`
	Amount string `json:"amount"`
}

type TotalsView struct {
	Subtotal    string    `json:"subtotal"`
	Discount    string    `json:"discount"`
	Tax         string    `json:"tax"`
	Total       string    `json:"total"`
	Taxes       []TaxView `json:"taxes"`
	AddonsTotal string    `json:"addons_total,omitempty"`
}

type DocumentView struct {
	Kind               string          `json:"kind"`
	ID                 uuid.UUID       `json:"id"`
	Number             *string         `json:"number"`
	Revision           int             `json:"revision"`
	Status             string          `json:"status"`
	Title              *string         `json:"title"`
	Currency           string          `json:"currency"`
	IssueDate          *models.Date    `json:"issue_date"`
	ValidUntil         *models.Date    `json:"valid_until,omitempty"`
	DueDate            *models.Date    `json:"due_date,omitempty"`
	Terms              *string         `json:"terms"`
	Notes              *string         `json:"notes"`
	Customer           CustomerRef     `json:"customer"`
	Template           TemplateRef     `json:"template"`
	BankAccountID      *uuid.UUID      `json:"bank_account_id"`
	EstimateID         *uuid.UUID      `json:"estimate_id,omitempty"`
	EstimateNumber     *string         `json:"estimate_number,omitempty"`
	PreviousRevisionID *uuid.UUID      `json:"previous_revision_id,omitempty"`
	Items              []ItemView      `json:"items"`
	Milestones         []MilestoneView `json:"milestones,omitempty"`
	Totals             TotalsView      `json:"totals"`
	Invoices           []DocSummary    `json:"invoices,omitempty"`
	Warnings           []string        `json:"warnings,omitempty"`
	FinalisedAt        *time.Time      `json:"finalised_at,omitempty"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
}

type DocSummary struct {
	Kind      string       `json:"kind"`
	ID        uuid.UUID    `json:"id"`
	Number    *string      `json:"number"`
	Revision  int          `json:"revision"`
	Status    string       `json:"status"`
	Title     *string      `json:"title"`
	Customer  CustomerRef  `json:"customer"`
	Total     string       `json:"total"`
	Currency  string       `json:"currency"`
	IssueDate *models.Date `json:"issue_date"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}

// ---------- neutral in-memory record ----------

type item struct {
	models.LineItem
	IsAddon bool
}

type docRecord struct {
	Kind       string
	H          models.DocumentHeader
	ValidUntil *models.Date
	DueDate    *models.Date
	EstimateID *uuid.UUID
	Items      []item
	Milestones []models.Milestone
}

func (r *docRecord) table() string {
	if r.Kind == KindEstimate {
		return "estimates"
	}
	return "invoices"
}

func loadDoc(tx *gorm.DB, kind string, id uuid.UUID, lock bool) (*docRecord, error) {
	q := tx
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	rec := &docRecord{Kind: kind}
	switch kind {
	case KindEstimate:
		var e models.Estimate
		if err := q.Omit("Items", "Milestones").First(&e, "id = ?", id).Error; err != nil {
			return nil, err
		}
		var items []models.EstimateItem
		if err := tx.Where("estimate_id = ?", id).Order("sort_order").Find(&items).Error; err != nil {
			return nil, err
		}
		if err := tx.Where("estimate_id = ?", id).Order("sort_order").Find(&rec.Milestones).Error; err != nil {
			return nil, err
		}
		rec.H, rec.ValidUntil = e.DocumentHeader, e.ValidUntil
		for _, it := range items {
			rec.Items = append(rec.Items, item{LineItem: it.LineItem, IsAddon: it.IsAddon})
		}
	case KindInvoice:
		var inv models.Invoice
		if err := q.Omit("Items").First(&inv, "id = ?", id).Error; err != nil {
			return nil, err
		}
		var items []models.InvoiceItem
		if err := tx.Where("invoice_id = ?", id).Order("sort_order").Find(&items).Error; err != nil {
			return nil, err
		}
		rec.H, rec.DueDate, rec.EstimateID = inv.DocumentHeader, inv.DueDate, inv.EstimateID
		for _, it := range items {
			rec.Items = append(rec.Items, item{LineItem: it.LineItem})
		}
	default:
		return nil, checkKind(kind)
	}
	return rec, nil
}

func saveHeader(tx *gorm.DB, rec *docRecord, create bool) error {
	switch rec.Kind {
	case KindEstimate:
		e := models.Estimate{DocumentHeader: rec.H, ValidUntil: rec.ValidUntil}
		var err error
		if create {
			err = tx.Omit("Items", "Milestones").Create(&e).Error
		} else {
			err = tx.Omit("Items", "Milestones", "CreatedAt").Save(&e).Error
		}
		rec.H = e.DocumentHeader
		return err
	default:
		inv := models.Invoice{DocumentHeader: rec.H, DueDate: rec.DueDate, EstimateID: rec.EstimateID}
		var err error
		if create {
			err = tx.Omit("Items").Create(&inv).Error
		} else {
			err = tx.Omit("Items", "CreatedAt").Save(&inv).Error
		}
		rec.H = inv.DocumentHeader
		return err
	}
}

func replaceChildren(tx *gorm.DB, rec *docRecord, items bool, milestones bool) error {
	if items {
		if rec.Kind == KindEstimate {
			if err := tx.Where("estimate_id = ?", rec.H.ID).Delete(&models.EstimateItem{}).Error; err != nil {
				return err
			}
			rows := make([]models.EstimateItem, len(rec.Items))
			for i, it := range rec.Items {
				it.SortOrder = i + 1
				it.TenantID = rec.H.TenantID
				rows[i] = models.EstimateItem{LineItem: it.LineItem, EstimateID: rec.H.ID, IsAddon: it.IsAddon}
			}
			if len(rows) > 0 {
				if err := tx.Create(&rows).Error; err != nil {
					return err
				}
			}
		} else {
			if err := tx.Where("invoice_id = ?", rec.H.ID).Delete(&models.InvoiceItem{}).Error; err != nil {
				return err
			}
			rows := make([]models.InvoiceItem, len(rec.Items))
			for i, it := range rec.Items {
				it.SortOrder = i + 1
				it.TenantID = rec.H.TenantID
				rows[i] = models.InvoiceItem{LineItem: it.LineItem, InvoiceID: rec.H.ID}
			}
			if len(rows) > 0 {
				if err := tx.Create(&rows).Error; err != nil {
					return err
				}
			}
		}
	}
	if milestones && rec.Kind == KindEstimate {
		if err := tx.Where("estimate_id = ?", rec.H.ID).Delete(&models.Milestone{}).Error; err != nil {
			return err
		}
		for i := range rec.Milestones {
			m := &rec.Milestones[i]
			m.ID, m.SortOrder, m.TenantID, m.EstimateID = uuid.Nil, i+1, rec.H.TenantID, rec.H.ID
		}
		if len(rec.Milestones) > 0 {
			if err := tx.Create(&rec.Milestones).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

// ---------- input resolution ----------

func resolveItems(kind string, in []ItemInput, rates map[uuid.UUID]models.TaxRate, def *models.TaxRate) ([]item, error) {
	out := make([]item, 0, len(in))
	for i, it := range in {
		n := i + 1
		desc := strings.TrimSpace(it.Description)
		if desc == "" {
			return nil, invalid("item %d: description is required", n)
		}
		if !it.Quantity.IsPositive() {
			return nil, invalid("item %d: quantity must be greater than 0", n)
		}
		if it.UnitPrice.IsNegative() {
			return nil, invalid("item %d: unit price cannot be negative", n)
		}
		dt := it.DiscountType
		if dt == "" {
			dt = "none"
			if !it.DiscountValue.IsZero() {
				return nil, invalid("item %d: set discount_type to percent or amount", n)
			}
		}
		switch dt {
		case "none":
			it.DiscountValue = zero
		case "percent":
			if it.DiscountValue.IsNegative() || it.DiscountValue.GreaterThan(hundred) {
				return nil, invalid("item %d: percent discount must be 0–100", n)
			}
		case "amount":
			if it.DiscountValue.IsNegative() {
				return nil, invalid("item %d: discount cannot be negative", n)
			}
			if it.DiscountValue.GreaterThan(it.Quantity.Mul(it.UnitPrice)) {
				return nil, invalid("item %d: discount is larger than the line amount", n)
			}
		default:
			return nil, invalid("item %d: discount_type must be none, percent or amount", n)
		}
		if it.IsAddon && kind != KindEstimate {
			return nil, invalid("item %d: optional add-ons are only allowed on estimates", n)
		}
		var taxID *uuid.UUID
		switch {
		case it.NoTax:
		case it.TaxRateID != nil:
			r, ok := rates[*it.TaxRateID]
			if !ok || !r.Active {
				return nil, invalid("item %d: tax rate %s not found or inactive", n, it.TaxRateID)
			}
			id := r.ID
			taxID = &id
		case def != nil:
			id := def.ID
			taxID = &id
		}
		out = append(out, item{
			LineItem: models.LineItem{
				Description:   desc,
				Quantity:      it.Quantity.Round(3),
				UnitPrice:     it.UnitPrice.Round(2),
				DiscountType:  dt,
				DiscountValue: it.DiscountValue.Round(2),
				TaxRateID:     taxID,
			},
			IsAddon: it.IsAddon,
		})
	}
	return out, nil
}

func resolveMilestones(in []MilestoneInput) ([]models.Milestone, error) {
	out := make([]models.Milestone, 0, len(in))
	for i, m := range in {
		name := strings.TrimSpace(m.Name)
		if name == "" {
			return nil, invalid("milestone %d: name is required", i+1)
		}
		if !m.Percent.IsPositive() || m.Percent.GreaterThan(hundred) {
			return nil, invalid("milestone %d: percent must be greater than 0 and at most 100", i+1)
		}
		out = append(out, models.Milestone{Name: name, Note: trimPtr(m.Note), Percent: m.Percent.Round(3), DueDate: m.DueDate})
	}
	return out, nil
}

// resolveTemplate returns the current version of a template of the right type
// (the tenant default when templateID is nil).
func resolveTemplate(tx *gorm.DB, kind string, templateID *uuid.UUID) (*models.Template, error) {
	var t models.Template
	q := tx.Where("doc_type = ? AND archived_at IS NULL", kind)
	if templateID != nil {
		q = q.Where("id = ?", *templateID)
	} else {
		q = q.Where("is_default")
	}
	if err := q.First(&t).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if templateID != nil {
				return nil, invalid("template %s not found, archived, or not an %s template", templateID, kind)
			}
			return nil, invalid("no default %s template; create one or pass template_id", kind)
		}
		return nil, err
	}
	if t.CurrentVersionID == nil {
		return nil, invalid("template %q has no version", t.Name)
	}
	return &t, nil
}

func requireCustomer(tx *gorm.DB, id uuid.UUID) (*models.Customer, error) {
	var c models.Customer
	if err := tx.First(&c, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, invalid("customer %s not found", id)
		}
		return nil, err
	}
	if c.ArchivedAt != nil {
		return nil, invalid("customer %q is archived", c.Name)
	}
	return &c, nil
}

func requireBank(tx *gorm.DB, id uuid.UUID) error {
	var b models.BankAccount
	if err := tx.First(&b, "id = ? AND active", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return invalid("bank account %s not found or inactive", id)
		}
		return err
	}
	return nil
}

// applyInput mutates a draft record from a DocumentInput.
func applyInput(tx *gorm.DB, rec *docRecord, in DocumentInput) (itemsChanged, msChanged bool, err error) {
	if in.CustomerID != nil {
		if _, err := requireCustomer(tx, *in.CustomerID); err != nil {
			return false, false, err
		}
		if rec.EstimateID != nil && *in.CustomerID != rec.H.CustomerID {
			return false, false, invalid("this invoice was created from an estimate; its customer cannot change")
		}
		rec.H.CustomerID = *in.CustomerID
	}
	if in.TemplateID != nil {
		t, err := resolveTemplate(tx, rec.Kind, in.TemplateID)
		if err != nil {
			return false, false, err
		}
		rec.H.TemplateVersionID = *t.CurrentVersionID
	}
	if in.ClearBank {
		rec.H.BankAccountID = nil
	} else if in.BankAccountID != nil {
		if err := requireBank(tx, *in.BankAccountID); err != nil {
			return false, false, err
		}
		rec.H.BankAccountID = in.BankAccountID
	}
	if in.Title != nil {
		rec.H.Title = trimPtr(in.Title)
	}
	if in.Terms != nil {
		rec.H.Terms = trimPtr(in.Terms)
	}
	if in.Notes != nil {
		rec.H.Notes = trimPtr(in.Notes)
	}
	if in.IssueDate != nil {
		rec.H.IssueDate = in.IssueDate
	}
	if in.ValidUntil != nil {
		if rec.Kind != KindEstimate {
			return false, false, invalid("valid_until applies to estimates; use due_date for invoices")
		}
		rec.ValidUntil = in.ValidUntil
	}
	if in.DueDate != nil {
		if rec.Kind != KindInvoice {
			return false, false, invalid("due_date applies to invoices; use valid_until for estimates")
		}
		rec.DueDate = in.DueDate
	}
	if rec.H.IssueDate != nil {
		if rec.ValidUntil != nil && rec.ValidUntil.Before(*rec.H.IssueDate) {
			return false, false, invalid("valid_until cannot be before the issue date")
		}
		if rec.DueDate != nil && rec.DueDate.Before(*rec.H.IssueDate) {
			return false, false, invalid("due_date cannot be before the issue date")
		}
	}
	if in.Items != nil {
		rates, def, err := loadTaxRates(tx)
		if err != nil {
			return false, false, err
		}
		items, err := resolveItems(rec.Kind, *in.Items, rates, def)
		if err != nil {
			return false, false, err
		}
		rec.Items, itemsChanged = items, true
	}
	if in.Milestones != nil {
		if rec.Kind != KindEstimate {
			return false, false, invalid("milestones are only allowed on estimates")
		}
		ms, err := resolveMilestones(*in.Milestones)
		if err != nil {
			return false, false, err
		}
		rec.Milestones, msChanged = ms, true
	}
	return itemsChanged, msChanged, nil
}

// ---------- CRUD ----------

func (s *Service) CreateDocument(ctx context.Context, a Actor, kind string, in DocumentInput) (*DocumentView, error) {
	if err := checkKind(kind); err != nil {
		return nil, err
	}
	if in.CustomerID == nil {
		return nil, invalid("customer_id is required")
	}
	var view *DocumentView
	err := s.tx(ctx, a, func(tx *gorm.DB) error {
		var tenant models.Tenant
		if err := tx.First(&tenant, "id = ?", a.TenantID).Error; err != nil {
			return err
		}
		t, err := resolveTemplate(tx, kind, in.TemplateID)
		if err != nil {
			return err
		}
		rec := &docRecord{Kind: kind, H: models.DocumentHeader{
			TenantID:          a.TenantID,
			TemplateVersionID: *t.CurrentVersionID,
			Status:            "draft",
			Currency:          tenant.Currency,
			Terms:             tenant.DefaultTerms,
		}}
		in.TemplateID = nil
		if _, _, err := applyInput(tx, rec, in); err != nil {
			return err
		}
		if err := saveHeader(tx, rec, true); err != nil {
			return err
		}
		if err := replaceChildren(tx, rec, true, true); err != nil {
			return err
		}
		if err := s.audit(tx, a, "create", kind, rec.H.ID, in); err != nil {
			return err
		}
		view, err = s.buildView(tx, kind, rec.H.ID)
		return err
	})
	return view, err
}

func (s *Service) UpdateDocument(ctx context.Context, a Actor, kind string, id uuid.UUID, in DocumentInput) (*DocumentView, error) {
	if err := checkKind(kind); err != nil {
		return nil, err
	}
	var view *DocumentView
	err := s.tx(ctx, a, func(tx *gorm.DB) error {
		rec, err := loadDoc(tx, kind, id, true)
		if err != nil {
			return err
		}
		if rec.H.Status != "draft" {
			return &LockedError{Message: fmt.Sprintf("%s %s is %s and cannot be edited; create a revision instead", kind, deref(rec.H.Number), rec.H.Status)}
		}
		itemsChanged, msChanged, err := applyInput(tx, rec, in)
		if err != nil {
			return err
		}
		if err := saveHeader(tx, rec, false); err != nil {
			return err
		}
		if err := replaceChildren(tx, rec, itemsChanged, msChanged); err != nil {
			return err
		}
		if err := s.audit(tx, a, "update", kind, id, in); err != nil {
			return err
		}
		view, err = s.buildView(tx, kind, id)
		return err
	})
	return view, err
}

func (s *Service) DeleteDraft(ctx context.Context, a Actor, kind string, id uuid.UUID) error {
	if err := checkKind(kind); err != nil {
		return err
	}
	return s.tx(ctx, a, func(tx *gorm.DB) error {
		rec, err := loadDoc(tx, kind, id, true)
		if err != nil {
			return err
		}
		if rec.H.Status != "draft" {
			return &LockedError{Message: "only drafts can be deleted"}
		}
		if err := tx.Exec("DELETE FROM "+rec.table()+" WHERE id = ?", id).Error; err != nil {
			return err
		}
		return s.audit(tx, a, "delete", kind, id, nil)
	})
}

func (s *Service) GetDocument(ctx context.Context, a Actor, kind string, id uuid.UUID) (*DocumentView, error) {
	if err := checkKind(kind); err != nil {
		return nil, err
	}
	var view *DocumentView
	err := s.tx(ctx, a, func(tx *gorm.DB) error {
		var err error
		view, err = s.buildView(tx, kind, id)
		return err
	})
	return view, err
}

type DocumentFilter struct {
	Query      string
	Status     string
	CustomerID *uuid.UUID
	EstimateID *uuid.UUID
	Limit      int
	Offset     int
}

// ListDocuments lists the latest documents. Superseded revisions are hidden
// unless Status == "superseded" is requested.
func (s *Service) ListDocuments(ctx context.Context, a Actor, kind string, f DocumentFilter) ([]DocSummary, error) {
	if err := checkKind(kind); err != nil {
		return nil, err
	}
	if f.Limit <= 0 || f.Limit > 100 {
		f.Limit = 30
	}
	var out []DocSummary
	err := s.tx(ctx, a, func(tx *gorm.DB) error {
		table := "estimates"
		if kind == KindInvoice {
			table = "invoices"
		}
		q := tx.Table(table + " d").Select("d.id").Joins("JOIN customers c ON c.id = d.customer_id")
		switch f.Status {
		case "":
			q = q.Where("d.status <> 'superseded'")
		case "draft", "final", "superseded":
			q = q.Where("d.status = ?", f.Status)
		default:
			return invalid("status must be draft, final or superseded")
		}
		if f.CustomerID != nil {
			q = q.Where("d.customer_id = ?", *f.CustomerID)
		}
		if f.EstimateID != nil && kind == KindInvoice {
			q = q.Where("d.estimate_id = ?", *f.EstimateID)
		}
		if qs := strings.TrimSpace(f.Query); qs != "" {
			like := "%" + escapeLike(strings.ToLower(qs)) + "%"
			q = q.Where("lower(coalesce(d.number,'') || ' ' || coalesce(d.title,'') || ' ' || c.name || ' ' || coalesce(c.company,'')) LIKE ?", like)
		}
		var ids []uuid.UUID
		if err := q.Order("d.created_at DESC").Limit(f.Limit).Offset(f.Offset).Pluck("d.id", &ids).Error; err != nil {
			return err
		}
		for _, id := range ids {
			sum, err := s.summary(tx, kind, id)
			if err != nil {
				return err
			}
			out = append(out, *sum)
		}
		return nil
	})
	if out == nil {
		out = []DocSummary{}
	}
	return out, err
}

func (s *Service) summary(tx *gorm.DB, kind string, id uuid.UUID) (*DocSummary, error) {
	rec, err := loadDoc(tx, kind, id, false)
	if err != nil {
		return nil, err
	}
	var c models.Customer
	if err := tx.First(&c, "id = ?", rec.H.CustomerID).Error; err != nil {
		return nil, err
	}
	total := rec.H.Total.Decimal
	if rec.H.Status == "draft" {
		rates, _, err := loadTaxRates(tx)
		if err != nil {
			return nil, err
		}
		_, t := calcTotals(linesFor(rec, rates))
		total = t.Total
	}
	return &DocSummary{
		Kind: kind, ID: rec.H.ID, Number: rec.H.Number, Revision: rec.H.Revision, Status: rec.H.Status,
		Title: rec.H.Title, Customer: CustomerRef{ID: c.ID, Name: c.Name, Company: c.Company},
		Total: money(total), Currency: rec.H.Currency, IssueDate: rec.H.IssueDate,
		CreatedAt: rec.H.CreatedAt, UpdatedAt: rec.H.UpdatedAt,
	}, nil
}

// linesFor builds totals input: frozen snapshots for issued docs, live tax rates for drafts.
func linesFor(rec *docRecord, rates map[uuid.UUID]models.TaxRate) []lineIn {
	lines := make([]lineIn, len(rec.Items))
	for i, it := range rec.Items {
		l := lineIn{Quantity: it.Quantity, UnitPrice: it.UnitPrice, DiscountType: it.DiscountType,
			DiscountValue: it.DiscountValue, IsAddon: it.IsAddon}
		if rec.H.Status != "draft" {
			if it.TaxRate.Valid {
				l.HasTax, l.TaxName, l.TaxRate = true, deref(it.TaxName), it.TaxRate.Decimal
			}
		} else if it.TaxRateID != nil {
			if r, ok := rates[*it.TaxRateID]; ok {
				l.HasTax, l.TaxName, l.TaxRate = true, r.Name, r.Rate
			}
		}
		lines[i] = l
	}
	return lines
}

func (s *Service) buildView(tx *gorm.DB, kind string, id uuid.UUID) (*DocumentView, error) {
	rec, err := loadDoc(tx, kind, id, false)
	if err != nil {
		return nil, err
	}
	rates, _, err := loadTaxRates(tx)
	if err != nil {
		return nil, err
	}
	var c models.Customer
	if err := tx.First(&c, "id = ?", rec.H.CustomerID).Error; err != nil {
		return nil, err
	}
	var tv models.TemplateVersion
	if err := tx.Select("id, template_id, version").First(&tv, "id = ?", rec.H.TemplateVersionID).Error; err != nil {
		return nil, err
	}
	var tmpl models.Template
	if err := tx.First(&tmpl, "id = ?", tv.TemplateID).Error; err != nil {
		return nil, err
	}

	lines := linesFor(rec, rates)
	outs, totals := calcTotals(lines)

	v := &DocumentView{
		Kind: kind, ID: rec.H.ID, Number: rec.H.Number, Revision: rec.H.Revision, Status: rec.H.Status,
		Title: rec.H.Title, Currency: rec.H.Currency, IssueDate: rec.H.IssueDate, ValidUntil: rec.ValidUntil,
		DueDate: rec.DueDate, Terms: rec.H.Terms, Notes: rec.H.Notes,
		Customer:      CustomerRef{ID: c.ID, Name: c.Name, Company: c.Company},
		Template:      TemplateRef{ID: tmpl.ID, Name: tmpl.Name, Version: tv.Version},
		BankAccountID: rec.H.BankAccountID, EstimateID: rec.EstimateID, PreviousRevisionID: rec.H.PreviousRevisionID,
		FinalisedAt: rec.H.FinalisedAt, CreatedAt: rec.H.CreatedAt, UpdatedAt: rec.H.UpdatedAt,
		Items: make([]ItemView, len(rec.Items)),
	}
	if rec.H.Status != "draft" {
		totals.Subtotal, totals.Discount = rec.H.Subtotal.Decimal, rec.H.DiscountTotal.Decimal
		totals.Tax, totals.Total = rec.H.TaxTotal.Decimal, rec.H.Total.Decimal
	}
	for i, it := range rec.Items {
		iv := ItemView{
			ID: it.ID, Description: it.Description, Quantity: formatQty(it.Quantity), UnitPrice: money(it.UnitPrice),
			DiscountType: it.DiscountType, DiscountValue: it.DiscountValue.String(), TaxRateID: it.TaxRateID,
			IsAddon: it.IsAddon, Gross: money(outs[i].Gross), DiscountAmount: money(outs[i].Discount),
			Net: money(outs[i].Net), Tax: money(outs[i].Tax),
		}
		if lines[i].HasTax {
			iv.TaxName, iv.TaxRate = lines[i].TaxName, lines[i].TaxRate.String()
		}
		v.Items[i] = iv
	}
	v.Totals = TotalsView{Subtotal: money(totals.Subtotal), Discount: money(totals.Discount),
		Tax: money(totals.Tax), Total: money(totals.Total), Taxes: []TaxView{}}
	for _, g := range totals.Taxes {
		v.Totals.Taxes = append(v.Totals.Taxes, TaxView{Name: g.Name, Rate: g.Rate.String(), Amount: money(g.Amount)})
	}
	if !totals.AddonsTotal.IsZero() {
		v.Totals.AddonsTotal = money(totals.AddonsTotal)
	}

	if kind == KindEstimate {
		pcts := make([]decimal.Decimal, len(rec.Milestones))
		sum := zero
		for i, m := range rec.Milestones {
			pcts[i] = m.Percent
			sum = sum.Add(m.Percent)
		}
		amounts := milestoneAmounts(totals.Total, pcts)
		for i, m := range rec.Milestones {
			mv := MilestoneView{ID: m.ID, Name: m.Name, Note: m.Note, Percent: m.Percent.String(),
				Amount: money(amounts[i]), DueDate: m.DueDate, InvoiceID: m.InvoiceID}
			if m.InvoiceID != nil {
				mv.InvoiceNumber = lookupNumber(tx, "invoices", *m.InvoiceID)
			}
			v.Milestones = append(v.Milestones, mv)
		}
		if len(rec.Milestones) > 0 && !sum.Equal(hundred) {
			v.Warnings = append(v.Warnings, fmt.Sprintf("milestone percentages total %s%%; they must total 100%% before finalising", sum.String()))
		}
		var invIDs []uuid.UUID
		tx.Model(&models.Invoice{}).Where("estimate_id = ? AND status <> 'superseded'", id).Order("created_at").Pluck("id", &invIDs)
		for _, iid := range invIDs {
			if sum, err := s.summary(tx, KindInvoice, iid); err == nil {
				v.Invoices = append(v.Invoices, *sum)
			}
		}
	}
	if rec.EstimateID != nil {
		v.EstimateNumber = lookupNumber(tx, "estimates", *rec.EstimateID)
	}
	if rec.H.Status == "draft" {
		nonAddon := 0
		for _, it := range rec.Items {
			if !it.IsAddon {
				nonAddon++
			}
		}
		if nonAddon == 0 {
			v.Warnings = append(v.Warnings, "no line items yet (add-ons alone don't count)")
		}
		if c.ArchivedAt != nil {
			v.Warnings = append(v.Warnings, "customer is archived")
		}
		if tmpl.CurrentVersionID != nil && *tmpl.CurrentVersionID != rec.H.TemplateVersionID {
			v.Warnings = append(v.Warnings, "template has a newer version; it will be used when previewing or finalising")
		}
	}
	return v, nil
}

func lookupNumber(tx *gorm.DB, table string, id uuid.UUID) *string {
	var nums []*string
	tx.Table(table).Where("id = ?", id).Pluck("number", &nums)
	if len(nums) == 1 {
		return nums[0]
	}
	return nil
}
