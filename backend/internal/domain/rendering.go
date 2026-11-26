package domain

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/claviq/desk/backend/internal/models"
	"github.com/claviq/desk/backend/internal/render"
	"github.com/claviq/desk/backend/internal/signing"
	"github.com/claviq/desk/backend/internal/storage"
)

// ---------- data handed to Typst (docs/template-data.md) ----------

type RenderDoc struct {
	Kind           string `json:"kind"`
	Title          string `json:"title"`   // "Estimate" / "Invoice"
	Subject        string `json:"subject"` // the document's own title
	Number         string `json:"number"`
	Revision       int    `json:"revision"`
	IsDraft        bool   `json:"is_draft"`
	IssueDate      string `json:"issue_date"`
	ValidUntil     string `json:"valid_until"`
	DueDate        string `json:"due_date"`
	Currency       string `json:"currency"`
	Terms          string `json:"terms"`
	Notes          string `json:"notes"`
	EstimateNumber string `json:"estimate_number"`
}

type RenderIssuer struct {
	Name      string `json:"name"`
	LegalName string `json:"legal_name"`
	RegNo     string `json:"reg_no"`
	TaxRegNo  string `json:"tax_reg_no"`
	Address   string `json:"address"`
	Phone     string `json:"phone"`
	Email     string `json:"email"`
	Logo      string `json:"logo"` // file name in the template root, "" if none
}

type RenderCustomer struct {
	Name     string `json:"name"`
	Company  string `json:"company"`
	Address  string `json:"address"`
	Phone    string `json:"phone"`
	Email    string `json:"email"`
	TaxRegNo string `json:"tax_reg_no"`
}

type RenderItem struct {
	No          int    `json:"no"`
	Description string `json:"description"`
	Quantity    string `json:"quantity"`
	UnitPrice   string `json:"unit_price"`
	Discount    string `json:"discount"` // "10% (−20,000.00)" / "−5,000.00" / ""
	Tax         string `json:"tax"`      // "VAT 18%" / ""
	// Amount is quantity × unit price, BEFORE discount and tax, so an amount column
	// adds up to totals.subtotal; discounts are listed once, in totals.discount.
	Amount         string `json:"amount"`
	DiscountAmount string `json:"discount_amount"` // "20,000.00" or ""
	Net            string `json:"net"`             // amount − discount, before tax
}

type RenderTax struct {
	Name   string `json:"name"`
	Rate   string `json:"rate"`
	Amount string `json:"amount"`
}

type RenderTotals struct {
	Subtotal string      `json:"subtotal"`
	Discount string      `json:"discount"` // "" when no discount
	Taxes    []RenderTax `json:"taxes"`
	Tax      string      `json:"tax"`
	Total    string      `json:"total"`
}

type RenderMilestone struct {
	Name    string `json:"name"`
	Note    string `json:"note"`
	Percent string `json:"percent"`
	Amount  string `json:"amount"`
	DueDate string `json:"due_date"`
}

type RenderBank struct {
	AccountName   string `json:"account_name"`
	AccountNumber string `json:"account_number"`
	Bank          string `json:"bank"`
	Branch        string `json:"branch"`
	Swift         string `json:"swift"`
}

type RenderData struct {
	Doc         RenderDoc         `json:"doc"`
	Issuer      RenderIssuer      `json:"issuer"`
	Customer    RenderCustomer    `json:"customer"`
	Items       []RenderItem      `json:"items"`
	Addons      []RenderItem      `json:"addons"`
	AddonsTotal string            `json:"addons_total"`
	Totals      RenderTotals      `json:"totals"`
	Milestones  []RenderMilestone `json:"milestones"`
	Bank        *RenderBank       `json:"bank"`
}

// buildJob assembles template source, data, logo and font dirs for a document.
// Drafts render live data with the template's current version; issued
// documents render only from their frozen snapshots.
func (s *Service) buildJob(ctx context.Context, tx *gorm.DB, rec *docRecord) (*render.Job, error) {
	if rec.H.Status == "draft" {
		if err := refreshTemplateVersion(tx, rec); err != nil {
			return nil, err
		}
	}
	var tv models.TemplateVersion
	if err := tx.First(&tv, "id = ?", rec.H.TemplateVersionID).Error; err != nil {
		return nil, err
	}

	var issuer models.IssuerSnapshot
	var cust models.CustomerSnapshot
	if rec.H.Status == "draft" {
		var t models.Tenant
		if err := tx.First(&t, "id = ?", rec.H.TenantID).Error; err != nil {
			return nil, err
		}
		bank, err := resolveBank(tx, rec.H.BankAccountID)
		if err != nil {
			return nil, err
		}
		issuer = issuerSnapshot(&t, bank).V
		var c models.Customer
		if err := tx.First(&c, "id = ?", rec.H.CustomerID).Error; err != nil {
			return nil, err
		}
		cust = customerSnapshot(&c).V
	} else {
		issuer, cust = rec.H.IssuerSnapshot.V, rec.H.CustomerSnapshot.V
	}

	rates, _, err := loadTaxRates(tx)
	if err != nil {
		return nil, err
	}
	lines := linesFor(rec, rates)
	outs, totals := calcTotals(lines)
	if rec.H.Status != "draft" {
		totals.Subtotal, totals.Discount = rec.H.Subtotal.Decimal, rec.H.DiscountTotal.Decimal
		totals.Tax, totals.Total = rec.H.TaxTotal.Decimal, rec.H.Total.Decimal
	}

	title := "Estimate"
	if rec.Kind == KindInvoice {
		title = "Invoice"
	}
	number := deref(rec.H.Number)
	if number == "" {
		number = "DRAFT"
	}
	issue := rec.H.IssueDate
	if issue == nil {
		d := models.Today()
		issue = &d
	}
	data := RenderData{
		Doc: RenderDoc{
			Kind: rec.Kind, Title: title, Subject: deref(rec.H.Title), Number: number, Revision: rec.H.Revision,
			IsDraft: rec.H.Status == "draft", IssueDate: formatDate(issue), ValidUntil: formatDate(rec.ValidUntil),
			DueDate: formatDate(rec.DueDate), Currency: rec.H.Currency, Terms: deref(rec.H.Terms), Notes: deref(rec.H.Notes),
		},
		Issuer: RenderIssuer{Name: issuer.Name, LegalName: deref(issuer.LegalName), RegNo: deref(issuer.RegNo),
			TaxRegNo: deref(issuer.TaxRegNo), Address: deref(issuer.Address), Phone: deref(issuer.Phone), Email: deref(issuer.Email)},
		Customer: RenderCustomer{Name: cust.Name, Company: deref(cust.Company), Address: deref(cust.Address),
			Phone: deref(cust.Phone), Email: deref(cust.Email), TaxRegNo: deref(cust.TaxRegNo)},
		Items: []RenderItem{}, Addons: []RenderItem{}, Milestones: []RenderMilestone{},
		Totals: RenderTotals{Subtotal: formatMoney(totals.Subtotal), Tax: formatMoney(totals.Tax),
			Total: formatMoney(totals.Total), Taxes: []RenderTax{}},
	}
	if rec.EstimateID != nil {
		data.Doc.EstimateNumber = deref(lookupNumber(tx, "estimates", *rec.EstimateID))
	}
	if !totals.Discount.IsZero() {
		data.Totals.Discount = formatMoney(totals.Discount)
	}
	for _, g := range totals.Taxes {
		data.Totals.Taxes = append(data.Totals.Taxes, RenderTax{Name: g.Name, Rate: g.Rate.String(), Amount: formatMoney(g.Amount)})
	}
	for i, it := range rec.Items {
		ri := RenderItem{Description: it.Description, Quantity: formatQty(it.Quantity),
			UnitPrice: formatMoney(it.UnitPrice), Amount: formatMoney(outs[i].Gross), Net: formatMoney(outs[i].Net)}
		if !outs[i].Discount.IsZero() {
			ri.DiscountAmount = formatMoney(outs[i].Discount)
			switch it.DiscountType {
			case "percent":
				ri.Discount = fmt.Sprintf("%s%% (−%s)", it.DiscountValue.String(), ri.DiscountAmount)
			case "amount":
				ri.Discount = "−" + ri.DiscountAmount
			}
		}
		if lines[i].HasTax {
			ri.Tax = fmt.Sprintf("%s %s%%", lines[i].TaxName, lines[i].TaxRate.String())
		}
		if it.IsAddon {
			ri.No = len(data.Addons) + 1
			data.Addons = append(data.Addons, ri)
		} else {
			ri.No = len(data.Items) + 1
			data.Items = append(data.Items, ri)
		}
	}
	if len(data.Addons) > 0 {
		data.AddonsTotal = formatMoney(totals.AddonsTotal)
	}
	if len(rec.Milestones) > 0 {
		pcts := make([]models.Milestone, len(rec.Milestones))
		copy(pcts, rec.Milestones)
		ps := make([]decimal.Decimal, len(pcts))
		for i, m := range pcts {
			ps[i] = m.Percent
		}
		amounts := milestoneAmounts(totals.Total, ps)
		for i, m := range rec.Milestones {
			data.Milestones = append(data.Milestones, RenderMilestone{Name: m.Name, Note: deref(m.Note),
				Percent: formatPercent(m.Percent), Amount: formatMoney(amounts[i]), DueDate: formatDate(m.DueDate)})
		}
	}
	if issuer.Bank != nil {
		data.Bank = &RenderBank{AccountName: issuer.Bank.AccountName, AccountNumber: issuer.Bank.AccountNumber,
			Bank: issuer.Bank.Bank, Branch: deref(issuer.Bank.Branch), Swift: deref(issuer.Bank.Swift)}
	}

	job := &render.Job{Source: tv.TypstSource, Data: data}
	if logo, ext, err := s.GetLogo(ctx, issuer.LogoPath); err == nil && logo != nil {
		name := "logo." + ext
		job.Assets = map[string][]byte{name: logo}
		data.Issuer.Logo = name
		job.Data = data
	} else if err != nil && !errors.Is(err, storage.ErrNotFound) {
		s.log.Warn("logo unavailable", "error", err)
	}
	dirs, err := s.fontDirs(ctx, tx, rec.H.TenantID)
	if err != nil {
		return nil, err
	}
	job.FontDirs = dirs
	return job, nil
}

// ---------- signed links ----------

type linkClaims struct {
	K string    `json:"k"` // e = estimate, i = invoice, t = template version
	I uuid.UUID `json:"i"`
	T uuid.UUID `json:"t"`
}

const linkTTL = 7 * 24 * time.Hour

type Links struct {
	PreviewURL string    `json:"preview_url"`
	PDFURL     string    `json:"pdf_url"`
	ExpiresAt  time.Time `json:"expires_at"`
}

func (s *Service) makeLinks(k string, id, tenantID uuid.UUID) (*Links, error) {
	tok, err := signing.Sign(s.links, linkClaims{K: k, I: id, T: tenantID}, linkTTL)
	if err != nil {
		return nil, err
	}
	return &Links{
		PreviewURL: s.publicURL + "/p/" + tok,
		PDFURL:     s.publicURL + "/api/public/p/" + tok + "/pdf",
		ExpiresAt:  time.Now().Add(linkTTL),
	}, nil
}

// DocumentLinks returns signed, expiring links that open on a phone without login.
func (s *Service) DocumentLinks(ctx context.Context, a Actor, kind string, id uuid.UUID) (*Links, error) {
	if err := checkKind(kind); err != nil {
		return nil, err
	}
	if err := s.tx(ctx, a, func(tx *gorm.DB) error {
		_, err := loadDoc(tx, kind, id, false)
		return err
	}); err != nil {
		return nil, err
	}
	return s.makeLinks(kind[:1], id, a.TenantID)
}

// PreviewInfo describes what a public preview link points at.
type PreviewInfo struct {
	Kind         string    `json:"kind"` // estimate | invoice | template
	ID           uuid.UUID `json:"id"`
	Title        string    `json:"title"`
	Number       string    `json:"number"`
	Status       string    `json:"status"`
	CustomerName string    `json:"customer_name,omitempty"`
	Total        string    `json:"total,omitempty"`
	Currency     string    `json:"currency,omitempty"`
	Pages        int       `json:"pages"`
	PDFURL       string    `json:"pdf_url"`
	Version      string    `json:"version"` // changes when the content changes (cache-busting)
}

type rendered struct {
	pdf   []byte
	pages [][]byte
	name  string
	info  PreviewInfo
}

// resolvePreview verifies a token and renders (or loads from cache) the document.
func (s *Service) resolvePreview(ctx context.Context, token string, wantPDF bool) (*rendered, error) {
	c, err := signing.Verify[linkClaims](s.links, token)
	if err != nil {
		return nil, ErrNotFound
	}
	a := SystemActor(c.T)
	var out *rendered
	err = s.tx(ctx, a, func(tx *gorm.DB) error {
		var job *render.Job
		info := PreviewInfo{ID: c.I, PDFURL: s.publicURL + "/api/public/p/" + token + "/pdf"}
		var storedPDF *string
		switch c.K {
		case "e", "i":
			kind := map[string]string{"e": KindEstimate, "i": KindInvoice}[c.K]
			rec, err := loadDoc(tx, kind, c.I, false)
			if err != nil {
				return err
			}
			job, err = s.buildJob(ctx, tx, rec)
			if err != nil {
				return err
			}
			data := job.Data.(RenderData)
			info.Kind, info.Title, info.Status = kind, data.Doc.Title, rec.H.Status
			if data.Doc.Subject != "" {
				info.Title = data.Doc.Title + " — " + data.Doc.Subject
			}
			info.Number, info.CustomerName = data.Doc.Number, data.Customer.Name
			info.Total, info.Currency = data.Totals.Total, rec.H.Currency
			if rec.H.Status != "draft" {
				storedPDF = rec.H.PDFPath
			}
		case "t":
			var tv models.TemplateVersion
			if err := tx.First(&tv, "id = ?", c.I).Error; err != nil {
				return err
			}
			var t models.Template
			if err := tx.First(&t, "id = ?", tv.TemplateID).Error; err != nil {
				return err
			}
			dirs, err := s.fontDirs(ctx, tx, c.T)
			if err != nil {
				return err
			}
			job = &render.Job{Source: tv.TypstSource, Data: SampleData(tv.DocType), FontDirs: dirs}
			info.Kind, info.Title, info.Status = "template", t.Name, "sample"
			info.Number = fmt.Sprintf("v%d", tv.Version)
		default:
			return ErrNotFound
		}

		key := cacheKey(job)
		info.Version = key[:12]
		if r, ok := s.previews.get(key); ok && (!wantPDF || r.pdf != nil) {
			out = r
			return nil
		}
		r := &rendered{info: info, name: safeFileName(info.Number) + ".pdf"}
		if wantPDF {
			if storedPDF != nil {
				b, err := s.store.Get(ctx, *storedPDF)
				if err != nil {
					return fmt.Errorf("load stored pdf: %w", err)
				}
				r.pdf = b
			} else {
				pj := *job
				pj.Format = render.PDF
				res, err := s.renderer.Render(ctx, pj)
				if err != nil {
					return err
				}
				r.pdf = res.PDF
			}
		}
		pj := *job
		pj.Format, pj.PPI = render.PNG, 110
		res, err := s.renderer.Render(ctx, pj)
		if err != nil {
			return err
		}
		r.pages = res.Pages
		r.info.Pages = len(res.Pages)
		s.previews.put(key, r)
		out = r
		return nil
	})
	return out, err
}

func (s *Service) PreviewInfo(ctx context.Context, token string) (*PreviewInfo, error) {
	r, err := s.resolvePreview(ctx, token, false)
	if err != nil {
		return nil, err
	}
	return &r.info, nil
}

func (s *Service) PreviewPage(ctx context.Context, token string, page int) ([]byte, error) {
	r, err := s.resolvePreview(ctx, token, false)
	if err != nil {
		return nil, err
	}
	if page < 1 || page > len(r.pages) {
		return nil, ErrNotFound
	}
	return r.pages[page-1], nil
}

// PreviewPDF returns the stored PDF for issued documents, or a fresh DRAFT render.
func (s *Service) PreviewPDF(ctx context.Context, token string) ([]byte, string, error) {
	r, err := s.resolvePreview(ctx, token, true)
	if err != nil {
		return nil, "", err
	}
	return r.pdf, r.name, nil
}

// DocumentPDF is the authenticated download (web app).
func (s *Service) DocumentPDF(ctx context.Context, a Actor, kind string, id uuid.UUID) ([]byte, string, error) {
	links, err := s.DocumentLinks(ctx, a, kind, id)
	if err != nil {
		return nil, "", err
	}
	tok := links.PDFURL[len(s.publicURL+"/api/public/p/") : len(links.PDFURL)-len("/pdf")]
	return s.PreviewPDF(ctx, tok)
}

func cacheKey(job *render.Job) string {
	h := sha256.New()
	h.Write([]byte(job.Source))
	b, _ := json.Marshal(job.Data)
	h.Write(b)
	for n, a := range job.Assets {
		h.Write([]byte(n))
		h.Write(a)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// ---------- tiny LRU ----------

type previewCache struct {
	mu    sync.Mutex
	max   int
	ttl   time.Duration
	ll    *list.List
	items map[string]*list.Element
}

type cacheEntry struct {
	key string
	val *rendered
	at  time.Time
}

func newPreviewCache(max int, ttl time.Duration) *previewCache {
	return &previewCache{max: max, ttl: ttl, ll: list.New(), items: map[string]*list.Element{}}
}

func (c *previewCache) get(k string) (*rendered, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[k]; ok {
		e := el.Value.(*cacheEntry)
		if time.Since(e.at) < c.ttl {
			c.ll.MoveToFront(el)
			return e.val, true
		}
		c.ll.Remove(el)
		delete(c.items, k)
	}
	return nil, false
}

func (c *previewCache) put(k string, v *rendered) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[k]; ok {
		c.ll.Remove(el)
	}
	c.items[k] = c.ll.PushFront(&cacheEntry{key: k, val: v, at: time.Now()})
	for c.ll.Len() > c.max {
		last := c.ll.Back()
		c.ll.Remove(last)
		delete(c.items, last.Value.(*cacheEntry).key)
	}
}
