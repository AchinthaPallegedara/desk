package domain

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/claviq/desk/backend/internal/models"
)

var (
	hundred = decimal.NewFromInt(100)
	zero    = decimal.Zero
)

// ---------- line & document totals ----------

type lineIn struct {
	Quantity      decimal.Decimal
	UnitPrice     decimal.Decimal
	DiscountType  string
	DiscountValue decimal.Decimal
	HasTax        bool
	TaxName       string
	TaxRate       decimal.Decimal
	IsAddon       bool
}

type lineOut struct {
	Gross    decimal.Decimal // qty × price
	Discount decimal.Decimal
	Net      decimal.Decimal // gross − discount (the "amount" column)
	Tax      decimal.Decimal
}

// calcLine rounds each component to 2 dp; tax is computed per line on the net.
func calcLine(l lineIn) lineOut {
	gross := l.Quantity.Mul(l.UnitPrice).Round(2)
	var disc decimal.Decimal
	switch l.DiscountType {
	case "percent":
		disc = gross.Mul(l.DiscountValue).Div(hundred).Round(2)
	case "amount":
		disc = l.DiscountValue.Round(2)
	}
	if disc.GreaterThan(gross) {
		disc = gross
	}
	net := gross.Sub(disc)
	var tax decimal.Decimal
	if l.HasTax {
		tax = net.Mul(l.TaxRate).Div(hundred).Round(2)
	}
	return lineOut{Gross: gross, Discount: disc, Net: net, Tax: tax}
}

type taxGroup struct {
	Name   string
	Rate   decimal.Decimal
	Amount decimal.Decimal
	Base   decimal.Decimal
}

type docTotals struct {
	Subtotal    decimal.Decimal // Σ gross (non-addon)
	Discount    decimal.Decimal
	Net         decimal.Decimal // subtotal − discount
	Tax         decimal.Decimal
	Total       decimal.Decimal
	Taxes       []taxGroup
	AddonsNet   decimal.Decimal
	AddonsTotal decimal.Decimal // add-ons incl. tax; never part of Total
}

func calcTotals(lines []lineIn) ([]lineOut, docTotals) {
	outs := make([]lineOut, len(lines))
	var t docTotals
	groups := map[string]*taxGroup{}
	var order []string
	for i, l := range lines {
		o := calcLine(l)
		outs[i] = o
		if l.IsAddon {
			t.AddonsNet = t.AddonsNet.Add(o.Net)
			t.AddonsTotal = t.AddonsTotal.Add(o.Net).Add(o.Tax)
			continue
		}
		t.Subtotal = t.Subtotal.Add(o.Gross)
		t.Discount = t.Discount.Add(o.Discount)
		t.Tax = t.Tax.Add(o.Tax)
		if l.HasTax {
			k := l.TaxName + "|" + l.TaxRate.String()
			g, ok := groups[k]
			if !ok {
				g = &taxGroup{Name: l.TaxName, Rate: l.TaxRate}
				groups[k] = g
				order = append(order, k)
			}
			g.Amount = g.Amount.Add(o.Tax)
			g.Base = g.Base.Add(o.Net)
		}
	}
	t.Net = t.Subtotal.Sub(t.Discount)
	t.Total = t.Net.Add(t.Tax)
	for _, k := range order {
		t.Taxes = append(t.Taxes, *groups[k])
	}
	return outs, t
}

// milestoneAmounts splits total by percentages; the last milestone takes the
// rounding remainder so the amounts always add up to the total.
func milestoneAmounts(total decimal.Decimal, percents []decimal.Decimal) []decimal.Decimal {
	out := make([]decimal.Decimal, len(percents))
	var sum, pct decimal.Decimal
	for i, p := range percents {
		pct = pct.Add(p)
		if i == len(percents)-1 && pct.Equal(hundred) {
			out[i] = total.Sub(sum)
			continue
		}
		out[i] = total.Mul(p).Div(hundred).Round(2)
		sum = sum.Add(out[i])
	}
	return out
}

// ---------- formatting for documents ----------

// formatMoney renders 1234567.5 as "1,234,567.50".
func formatMoney(d decimal.Decimal) string {
	s := d.StringFixed(2)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	intPart, frac, _ := strings.Cut(s, ".")
	var b strings.Builder
	for i, c := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	out := b.String() + "." + frac
	if neg {
		out = "-" + out
	}
	return out
}

func formatQty(d decimal.Decimal) string { return d.String() }

func formatPercent(d decimal.Decimal) string { return d.String() }

func formatDate(d *models.Date) string {
	if d == nil {
		return ""
	}
	return d.Format("02 Jan 2006")
}

func money(d decimal.Decimal) string { return d.StringFixed(2) }

// ---------- numbering ----------

var seqToken = regexp.MustCompile(`\{seq(?::(\d+))?\}`)

// formatNumber expands {prefix} {yyyy} {yy} {mm} {seq} {seq:N}.
func formatNumber(format, prefix string, issue models.Date, n int) string {
	out := strings.NewReplacer(
		"{prefix}", prefix,
		"{yyyy}", issue.Format("2006"),
		"{yy}", issue.Format("06"),
		"{mm}", issue.Format("01"),
	).Replace(format)
	return seqToken.ReplaceAllStringFunc(out, func(m string) string {
		sub := seqToken.FindStringSubmatch(m)
		if sub[1] == "" {
			return strconv.Itoa(n)
		}
		w, _ := strconv.Atoi(sub[1])
		return fmt.Sprintf("%0*d", w, n)
	})
}

func lockSequence(tx *gorm.DB, docType string, seq *models.NumberSequence) error {
	return tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("doc_type = ?", docType).First(seq).Error
}

// issueNumber allocates the next number. Must run inside the finalise
// transaction: the row lock serialises concurrent finalisations, and a rollback
// (e.g. PDF render failure) returns the number, so sequences stay gapless.
func issueNumber(tx *gorm.DB, docType string, issue models.Date) (string, error) {
	var seq models.NumberSequence
	if err := lockSequence(tx, docType, &seq); err != nil {
		return "", err
	}
	year := issue.Year()
	if seq.ResetPolicy == "yearly" && (seq.CurrentPeriod == nil || *seq.CurrentPeriod != year) {
		seq.NextValue = 1
		seq.CurrentPeriod = &year
	} else if seq.CurrentPeriod == nil {
		seq.CurrentPeriod = &year
	}
	num := formatNumber(seq.Format, seq.Prefix, issue, seq.NextValue)
	seq.NextValue++
	if err := tx.Save(&seq).Error; err != nil {
		return "", err
	}
	return num, nil
}

// ---------- tax-rate lookup ----------

func loadTaxRates(tx *gorm.DB) (map[uuid.UUID]models.TaxRate, *models.TaxRate, error) {
	var rates []models.TaxRate
	if err := tx.Find(&rates).Error; err != nil {
		return nil, nil, err
	}
	m := make(map[uuid.UUID]models.TaxRate, len(rates))
	var def *models.TaxRate
	for i, r := range rates {
		m[r.ID] = r
		if r.IsDefault && r.Active {
			def = &rates[i]
		}
	}
	return m, def, nil
}
