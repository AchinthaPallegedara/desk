package domain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/claviq/desk/backend/internal/models"
	"github.com/claviq/desk/backend/internal/render"
)

// Finalise issues a draft: assigns the number, freezes snapshots, renders and
// stores the PDF, and locks the document — all in one transaction. If anything
// fails (including PDF rendering) nothing is committed and no number is used.
func (s *Service) Finalise(ctx context.Context, a Actor, kind string, id uuid.UUID) (*DocumentView, error) {
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
			return &LockedError{Message: fmt.Sprintf("%s %s is already %s", kind, deref(rec.H.Number), rec.H.Status)}
		}
		var tenant models.Tenant
		if err := tx.First(&tenant, "id = ?", a.TenantID).Error; err != nil {
			return err
		}
		cust, err := requireCustomer(tx, rec.H.CustomerID)
		if err != nil {
			return err
		}

		// Validation.
		nonAddon := 0
		for _, it := range rec.Items {
			if !it.IsAddon {
				nonAddon++
			}
		}
		if nonAddon == 0 {
			return invalid("add at least one line item before finalising")
		}
		if len(rec.Milestones) > 0 {
			sum := zero
			for _, m := range rec.Milestones {
				sum = sum.Add(m.Percent)
			}
			if !sum.Equal(hundred) {
				return invalid("milestone percentages total %s%%; they must total exactly 100%%", sum.String())
			}
		}

		// Drafts follow template edits: issue with the template's current version.
		if err := refreshTemplateVersion(tx, rec); err != nil {
			return err
		}

		// Dates.
		if rec.H.IssueDate == nil {
			d := models.Today()
			rec.H.IssueDate = &d
		}
		if kind == KindEstimate && rec.ValidUntil == nil {
			d := rec.H.IssueDate.AddDays(tenant.DefaultValidityDays)
			rec.ValidUntil = &d
		}
		if kind == KindInvoice && rec.DueDate == nil && tenant.DefaultPaymentDays > 0 {
			d := rec.H.IssueDate.AddDays(tenant.DefaultPaymentDays)
			rec.DueDate = &d
		}

		// Supersede the previous revision first (only one final per number).
		if rec.H.PreviousRevisionID != nil {
			if err := tx.Exec("UPDATE "+rec.table()+" SET status = 'superseded' WHERE id = ? AND status = 'final'",
				*rec.H.PreviousRevisionID).Error; err != nil {
				return err
			}
		}

		// Number: revisions keep their number; new documents take the next one.
		if rec.H.Number == nil {
			num, err := issueNumber(tx, kind, *rec.H.IssueDate)
			if err != nil {
				return err
			}
			rec.H.Number = &num
		}

		// Freeze line snapshots and totals.
		rates, _, err := loadTaxRates(tx)
		if err != nil {
			return err
		}
		lines := linesFor(rec, rates)
		outs, totals := calcTotals(lines)
		for i := range rec.Items {
			it := &rec.Items[i]
			it.LineTotal = decimal.NewNullDecimal(outs[i].Net)
			if lines[i].HasTax {
				name := lines[i].TaxName
				it.TaxName = &name
				it.TaxRate = decimal.NewNullDecimal(lines[i].TaxRate)
			} else {
				it.TaxName, it.TaxRate = nil, decimal.NullDecimal{}
			}
		}
		rec.H.Subtotal = decimal.NewNullDecimal(totals.Subtotal)
		rec.H.DiscountTotal = decimal.NewNullDecimal(totals.Discount)
		rec.H.TaxTotal = decimal.NewNullDecimal(totals.Tax)
		rec.H.Total = decimal.NewNullDecimal(totals.Total)

		// Freeze parties.
		cs := customerSnapshot(cust)
		rec.H.CustomerSnapshot = &cs
		bank, err := resolveBank(tx, rec.H.BankAccountID)
		if err != nil {
			return err
		}
		if bank != nil && rec.H.BankAccountID == nil {
			rec.H.BankAccountID = &bank.ID
		}
		is := issuerSnapshot(&tenant, bank)
		rec.H.IssuerSnapshot = &is

		// Children are written while the parent is still a draft (triggers lock them after).
		if err := replaceChildrenKeepMilestones(tx, rec); err != nil {
			return err
		}

		// Render the final PDF from exactly the frozen data.
		rec.H.Status = "final"
		job, err := s.buildJob(ctx, tx, rec)
		if err != nil {
			return err
		}
		job.Format = render.PDF
		res, err := s.renderer.Render(ctx, *job)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(res.PDF)
		key := fmt.Sprintf("tenants/%s/%ss/%s-r%d-%s.pdf", a.TenantID, kind, safeFileName(*rec.H.Number), rec.H.Revision, rec.H.ID)
		if err := s.store.Put(ctx, key, res.PDF, "application/pdf"); err != nil {
			return fmt.Errorf("store pdf: %w", err)
		}
		now := time.Now()
		hash := hex.EncodeToString(sum[:])
		rec.H.PDFPath, rec.H.PDFSHA256 = &key, &hash
		rec.H.FinalisedAt, rec.H.FinalisedBy = &now, a.UserID

		if err := saveHeader(tx, rec, false); err != nil {
			return err
		}

		// A revised milestone invoice keeps its milestone link.
		if kind == KindInvoice && rec.H.PreviousRevisionID != nil {
			if err := tx.Model(&models.Milestone{}).Where("invoice_id = ?", *rec.H.PreviousRevisionID).
				Update("invoice_id", rec.H.ID).Error; err != nil {
				return err
			}
		}

		if err := s.audit(tx, a, "finalise", kind, id, map[string]any{
			"number": rec.H.Number, "revision": rec.H.Revision, "total": money(totals.Total), "pdf_sha256": hash,
		}); err != nil {
			return err
		}
		view, err = s.buildView(tx, kind, id)
		return err
	})
	return view, err
}

func replaceChildrenKeepMilestones(tx *gorm.DB, rec *docRecord) error {
	return replaceChildren(tx, rec, true, false)
}

func refreshTemplateVersion(tx *gorm.DB, rec *docRecord) error {
	var current []uuid.UUID
	err := tx.Table("template_versions v").Joins("JOIN templates t ON t.id = v.template_id").
		Where("v.id = ? AND t.current_version_id IS NOT NULL", rec.H.TemplateVersionID).
		Pluck("t.current_version_id", &current).Error
	if err != nil {
		return err
	}
	if len(current) == 1 {
		rec.H.TemplateVersionID = current[0]
	}
	return nil
}

func resolveBank(tx *gorm.DB, id *uuid.UUID) (*models.BankAccount, error) {
	var b models.BankAccount
	q := tx.Where("active")
	if id != nil {
		q = q.Where("id = ?", *id)
	} else {
		q = q.Where("is_default")
	}
	if err := q.First(&b).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if id != nil {
				return nil, invalid("bank account %s is inactive or missing", id)
			}
			return nil, nil
		}
		return nil, err
	}
	return &b, nil
}

func customerSnapshot(c *models.Customer) models.JSONB[models.CustomerSnapshot] {
	return models.NewJSONB(models.CustomerSnapshot{Name: c.Name, Company: c.Company, Address: c.Address,
		Phone: c.Phone, Email: c.Email, TaxRegNo: c.TaxRegNo})
}

func issuerSnapshot(t *models.Tenant, b *models.BankAccount) models.JSONB[models.IssuerSnapshot] {
	is := models.IssuerSnapshot{Name: t.Name, LegalName: t.LegalName, RegNo: t.RegNo, TaxRegNo: t.TaxRegNo,
		Address: t.Address, Phone: t.Phone, Email: t.Email, LogoPath: t.LogoPath}
	if b != nil {
		is.Bank = &models.BankSnapshot{AccountName: b.AccountName, AccountNumber: b.AccountNumber,
			Bank: b.Bank, Branch: b.Branch, Swift: b.Swift}
	}
	return models.NewJSONB(is)
}

func safeFileName(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, s)
}

// Revise creates a new draft revision of a final document (same number,
// revision + 1). The final stays valid until the revision is finalised.
func (s *Service) Revise(ctx context.Context, a Actor, kind string, id uuid.UUID) (*DocumentView, error) {
	if err := checkKind(kind); err != nil {
		return nil, err
	}
	var view *DocumentView
	err := s.tx(ctx, a, func(tx *gorm.DB) error {
		rec, err := loadDoc(tx, kind, id, true)
		if err != nil {
			return err
		}
		if rec.H.Status != "final" {
			return invalid("only final documents can be revised (this one is %s)", rec.H.Status)
		}
		var existing []uuid.UUID
		if err := tx.Table(rec.table()).Where("previous_revision_id = ?", id).Pluck("id", &existing).Error; err != nil {
			return err
		}
		if len(existing) > 0 {
			return &ConflictError{Message: fmt.Sprintf("a draft revision already exists (id %s); edit that instead", existing[0])}
		}
		if kind == KindEstimate {
			var n int64
			tx.Model(&models.Invoice{}).Where("estimate_id = ?", id).Count(&n)
			if n > 0 {
				return invalid("invoices have already been created from this estimate, so it can't be revised; create a new estimate instead")
			}
		}

		prevID := rec.H.ID
		nr := &docRecord{Kind: kind, ValidUntil: nil, DueDate: nil, EstimateID: rec.EstimateID}
		nr.H = models.DocumentHeader{
			TenantID: a.TenantID, CustomerID: rec.H.CustomerID, TemplateVersionID: rec.H.TemplateVersionID,
			BankAccountID: rec.H.BankAccountID, Number: rec.H.Number, Revision: rec.H.Revision + 1,
			PreviousRevisionID: &prevID, Status: "draft", Title: rec.H.Title, Currency: rec.H.Currency,
			Terms: rec.H.Terms, Notes: rec.H.Notes,
		}
		for _, it := range rec.Items {
			li := it.LineItem
			li.ID, li.TaxName, li.TaxRate, li.LineTotal = uuid.Nil, nil, decimal.NullDecimal{}, decimal.NullDecimal{}
			nr.Items = append(nr.Items, item{LineItem: li, IsAddon: it.IsAddon})
		}
		for _, m := range rec.Milestones {
			nr.Milestones = append(nr.Milestones, models.Milestone{Name: m.Name, Note: m.Note, Percent: m.Percent, DueDate: m.DueDate})
		}
		if err := saveHeader(tx, nr, true); err != nil {
			return err
		}
		if err := replaceChildren(tx, nr, true, true); err != nil {
			return err
		}
		if err := s.audit(tx, a, "revise", kind, nr.H.ID, map[string]any{"from": prevID, "revision": nr.H.Revision}); err != nil {
			return err
		}
		view, err = s.buildView(tx, kind, nr.H.ID)
		return err
	})
	return view, err
}

type ConvertInput struct {
	// Mode: "full" (one invoice for the whole estimate) or "milestones" (one invoice per milestone).
	Mode string `json:"mode"`
	// MilestoneIDs limits "milestones" mode to these (default: all unbilled).
	MilestoneIDs []uuid.UUID `json:"milestone_ids"`
	// AddonItemIDs: optional add-ons the client accepted ("full" mode); they become normal lines.
	AddonItemIDs []uuid.UUID `json:"addon_item_ids"`
	TemplateID   *uuid.UUID  `json:"template_id"`
}

// ConvertToInvoices creates draft invoice(s) from a final estimate.
func (s *Service) ConvertToInvoices(ctx context.Context, a Actor, estimateID uuid.UUID, in ConvertInput) ([]DocumentView, error) {
	var views []DocumentView
	err := s.tx(ctx, a, func(tx *gorm.DB) error {
		est, err := loadDoc(tx, KindEstimate, estimateID, true)
		if err != nil {
			return err
		}
		if est.H.Status != "final" {
			return invalid("finalise the estimate before converting it to an invoice")
		}
		var tenant models.Tenant
		if err := tx.First(&tenant, "id = ?", a.TenantID).Error; err != nil {
			return err
		}
		t, err := resolveTemplate(tx, KindInvoice, in.TemplateID)
		if err != nil {
			return err
		}
		var existing int64
		tx.Model(&models.Invoice{}).Where("estimate_id = ?", estimateID).Count(&existing)
		billed := 0
		for _, m := range est.Milestones {
			if m.InvoiceID != nil {
				billed++
			}
		}

		newInvoice := func(title *string, due *models.Date) *docRecord {
			eid := estimateID
			return &docRecord{Kind: KindInvoice, EstimateID: &eid, DueDate: due, H: models.DocumentHeader{
				TenantID: a.TenantID, CustomerID: est.H.CustomerID, TemplateVersionID: *t.CurrentVersionID,
				BankAccountID: est.H.BankAccountID, Status: "draft", Title: title, Currency: est.H.Currency,
				Terms: tenant.DefaultTerms,
			}}
		}
		var created []uuid.UUID

		switch in.Mode {
		case "", "full":
			if existing > 0 {
				return invalid("invoices already exist for this estimate; use milestones mode or edit the existing invoice")
			}
			accepted := map[uuid.UUID]bool{}
			for _, id := range in.AddonItemIDs {
				accepted[id] = true
			}
			inv := newInvoice(est.H.Title, nil)
			for _, it := range est.Items {
				if it.IsAddon && !accepted[it.ID] {
					continue
				}
				li := it.LineItem
				li.ID, li.TaxName, li.TaxRate, li.LineTotal = uuid.Nil, nil, decimal.NullDecimal{}, decimal.NullDecimal{}
				inv.Items = append(inv.Items, item{LineItem: li})
				delete(accepted, it.ID)
			}
			if len(accepted) > 0 {
				return invalid("addon_item_ids contains items that are not add-ons on this estimate")
			}
			if err := saveHeader(tx, inv, true); err != nil {
				return err
			}
			if err := replaceChildren(tx, inv, true, false); err != nil {
				return err
			}
			created = append(created, inv.H.ID)

		case "milestones":
			if len(est.Milestones) == 0 {
				return invalid("this estimate has no milestones")
			}
			if existing > int64(billed) {
				return invalid("a full invoice already exists for this estimate")
			}
			want := map[uuid.UUID]bool{}
			for _, id := range in.MilestoneIDs {
				want[id] = true
			}
			// Pre-tax base per tax rate on the estimate (non-addon lines), from frozen snapshots.
			type base struct {
				taxRateID *uuid.UUID
				net       decimal.Decimal
			}
			var bases []base
			idx := map[string]int{}
			for _, it := range est.Items {
				if it.IsAddon {
					continue
				}
				k := "none"
				if it.TaxRateID != nil {
					k = it.TaxRateID.String()
				}
				if _, ok := idx[k]; !ok {
					idx[k] = len(bases)
					bases = append(bases, base{taxRateID: it.TaxRateID})
				}
				bases[idx[k]].net = bases[idx[k]].net.Add(it.LineTotal.Decimal)
			}
			filtered := len(want) > 0
			for _, m := range est.Milestones {
				if filtered && !want[m.ID] {
					continue
				}
				delete(want, m.ID)
				if m.InvoiceID != nil {
					if len(in.MilestoneIDs) > 0 {
						return invalid("milestone %q is already invoiced", m.Name)
					}
					continue
				}
				title := fmt.Sprintf("%s — %s", m.Name, deref(est.H.Title))
				if est.H.Title == nil {
					title = m.Name
				}
				inv := newInvoice(&title, m.DueDate)
				for _, b := range bases {
					desc := fmt.Sprintf("%s — %s%% of estimate %s", m.Name, m.Percent.String(), deref(est.H.Number))
					inv.Items = append(inv.Items, item{LineItem: models.LineItem{
						Description: desc, Quantity: decimal.NewFromInt(1),
						UnitPrice: b.net.Mul(m.Percent).Div(hundred).Round(2), DiscountType: "none",
						TaxRateID: b.taxRateID,
					}})
				}
				if err := saveHeader(tx, inv, true); err != nil {
					return err
				}
				if err := replaceChildren(tx, inv, true, false); err != nil {
					return err
				}
				if err := tx.Model(&models.Milestone{}).Where("id = ?", m.ID).Update("invoice_id", inv.H.ID).Error; err != nil {
					return err
				}
				created = append(created, inv.H.ID)
			}
			if len(want) > 0 {
				return invalid("milestone_ids contains milestones that are not on this estimate")
			}
			if len(created) == 0 {
				return invalid("every milestone is already invoiced")
			}
		default:
			return invalid("mode must be full or milestones")
		}

		if err := s.audit(tx, a, "convert", KindEstimate, estimateID, map[string]any{"mode": in.Mode, "invoices": created}); err != nil {
			return err
		}
		for _, id := range created {
			v, err := s.buildView(tx, KindInvoice, id)
			if err != nil {
				return err
			}
			views = append(views, *v)
		}
		return nil
	})
	return views, err
}
