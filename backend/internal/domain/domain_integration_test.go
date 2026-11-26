package domain_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"errors"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/claviq/desk/backend/internal/database"
	"github.com/claviq/desk/backend/internal/domain"
	"github.com/claviq/desk/backend/internal/models"
	"github.com/claviq/desk/backend/internal/render"
	"github.com/claviq/desk/backend/internal/storage"
)

// Integration tests run against the local dev database (make db-up && make migrate).
// They are skipped when DATABASE_URL / MIGRATE_DATABASE_URL are not set.

type env struct {
	svc   *domain.Service
	app   *gorm.DB
	owner *gorm.DB
}

func setup(t *testing.T) *env {
	t.Helper()
	appURL, ownerURL := os.Getenv("DATABASE_URL"), os.Getenv("MIGRATE_DATABASE_URL")
	if appURL == "" || ownerURL == "" {
		t.Skip("DATABASE_URL and MIGRATE_DATABASE_URL not set")
	}
	app, err := database.Open(appURL, false)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := database.Open(ownerURL, false)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	store, _ := storage.NewLocal(dir + "/storage")
	svc := domain.New(domain.Options{DB: app, Renderer: render.New("typst"), Storage: store, DataDir: dir,
		AppSecret: "test-secret-test-secret-test-secret-123", PublicURL: "http://localhost:3000",
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err := svc.SyncBundledFonts(context.Background()); err != nil {
		t.Fatal(err)
	}
	return &env{svc: svc, app: app, owner: owner}
}

func (e *env) tenant(t *testing.T) domain.Actor {
	t.Helper()
	tn, u, err := domain.BootstrapTenant(context.Background(), e.owner, domain.NewTenant{
		BusinessName: "Test Co", OwnerEmail: uuid.NewString() + "@test.local", PasswordHash: "x"})
	if err != nil {
		t.Fatal(err)
	}
	return domain.Actor{TenantID: tn.ID, UserID: &u.ID, Via: "web"}
}

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func TestEstimateLifecycle(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	a := e.tenant(t)

	name := "Nimal Perera"
	cust, err := e.svc.CreateCustomer(ctx, a, domain.CustomerInput{Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	if found, _ := e.svc.ListCustomers(ctx, a, domain.CustomerFilter{Query: "nimal"}); len(found) != 1 {
		t.Fatalf("search: want 1 customer, got %d", len(found))
	}

	title := "Website"
	items := []domain.ItemInput{
		{Description: "Design", Quantity: d("1"), UnitPrice: d("100000")},
		{Description: "Dev", Quantity: d("10"), UnitPrice: d("5000"), DiscountType: "percent", DiscountValue: d("10")},
		{Description: "Hosting", Quantity: d("1"), UnitPrice: d("20000"), IsAddon: true},
	}
	ms := []domain.MilestoneInput{{Name: "Start", Percent: d("30")}, {Name: "End", Percent: d("60")}}
	est, err := e.svc.CreateDocument(ctx, a, domain.KindEstimate, domain.DocumentInput{
		CustomerID: &cust.ID, Title: &title, Items: &items, Milestones: &ms})
	if err != nil {
		t.Fatal(err)
	}
	// 100000 + 45000 net = 145000; VAT 18% = 26100; total 171100. Add-on excluded.
	if est.Totals.Total != "171100.00" || est.Totals.Discount != "5000.00" || est.Totals.AddonsTotal != "23600.00" {
		t.Fatalf("totals: %+v", est.Totals)
	}
	if len(est.Warnings) == 0 {
		t.Fatal("expected milestone warning (90%)")
	}
	if est.Number != nil {
		t.Fatal("draft must not have a number")
	}

	// Finalising with milestones at 90% fails and consumes no number.
	if _, err := e.svc.Finalise(ctx, a, domain.KindEstimate, est.ID); err == nil {
		t.Fatal("finalise should fail: milestones total 90")
	}
	ms[1].Percent = d("70")
	if _, err := e.svc.UpdateDocument(ctx, a, domain.KindEstimate, est.ID, domain.DocumentInput{Milestones: &ms}); err != nil {
		t.Fatal(err)
	}

	// Preview renders PNG pages.
	links, err := e.svc.DocumentLinks(ctx, a, domain.KindEstimate, est.ID)
	if err != nil {
		t.Fatal(err)
	}
	tok := links.PreviewURL[len("http://localhost:3000/p/"):]
	info, err := e.svc.PreviewInfo(ctx, tok)
	if err != nil || info.Pages < 1 || info.Number != "DRAFT" {
		t.Fatalf("preview: %+v %v", info, err)
	}

	fin, err := e.svc.Finalise(ctx, a, domain.KindEstimate, est.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fin.Number == nil || *fin.Number == "" || fin.Status != "final" {
		t.Fatalf("finalised: %+v", fin)
	}
	if fin.Milestones[0].Amount != "51330.00" || fin.Milestones[1].Amount != "119770.00" {
		t.Fatalf("milestone amounts: %+v", fin.Milestones)
	}

	// Locked: service refuses, and the database refuses even a direct write.
	if _, err := e.svc.UpdateDocument(ctx, a, domain.KindEstimate, est.ID, domain.DocumentInput{Title: &title}); !errors.Is(err, domain.ErrLocked) {
		t.Fatalf("update final: want ErrLocked, got %v", err)
	}
	err = database.WithTenant(ctx, e.app, a.TenantID, func(tx *gorm.DB) error {
		return tx.Exec("UPDATE estimate_items SET unit_price = 1 WHERE estimate_id = ?", est.ID).Error
	})
	if err == nil {
		t.Fatal("trigger should block item edits on a final estimate")
	}

	// Final PDF is downloadable.
	pdf, _, err := e.svc.PreviewPDF(ctx, tok)
	if err != nil || len(pdf) < 1000 || string(pdf[:4]) != "%PDF" {
		t.Fatalf("pdf: %d bytes, %v", len(pdf), err)
	}

	// Revision keeps the number; finalising supersedes the original.
	rev, err := e.svc.Revise(ctx, a, domain.KindEstimate, est.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rev.Revision != 1 || *rev.Number != *fin.Number {
		t.Fatalf("revision: %+v", rev)
	}
	if _, err := e.svc.Revise(ctx, a, domain.KindEstimate, est.ID); err == nil {
		t.Fatal("second draft revision should conflict")
	}
	rev2, err := e.svc.Finalise(ctx, a, domain.KindEstimate, rev.ID)
	if err != nil {
		t.Fatal(err)
	}
	old, _ := e.svc.GetDocument(ctx, a, domain.KindEstimate, est.ID)
	if old.Status != "superseded" {
		t.Fatalf("old status %s", old.Status)
	}

	// Convert one selected milestone: exactly one invoice (regression: selection used to leak).
	first := rev2.Milestones[0].ID
	invs, err := e.svc.ConvertToInvoices(ctx, a, rev2.ID, domain.ConvertInput{Mode: "milestones", MilestoneIDs: []uuid.UUID{first}})
	if err != nil {
		t.Fatal(err)
	}
	if len(invs) != 1 || invs[0].Totals.Total != "51330.00" {
		t.Fatalf("selected milestone: %d invoices, first total %s", len(invs), invs[0].Totals.Total)
	}
	// Then the rest (all unbilled).
	rest, err := e.svc.ConvertToInvoices(ctx, a, rev2.ID, domain.ConvertInput{Mode: "milestones"})
	if err != nil || len(rest) != 1 || rest[0].Totals.Total != "119770.00" {
		t.Fatalf("remaining milestones: %v %d", err, len(rest))
	}
	if _, err := e.svc.ConvertToInvoices(ctx, a, rev2.ID, domain.ConvertInput{Mode: "full"}); err == nil {
		t.Fatal("full conversion after milestone invoices should fail")
	}
	inv, err := e.svc.Finalise(ctx, a, domain.KindInvoice, invs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if inv.DueDate == nil || inv.EstimateNumber == nil {
		t.Fatalf("invoice: %+v", inv)
	}

	// Audit trail recorded.
	logs, _ := e.svc.ListAudit(ctx, a, "estimate", est.ID.String(), 50)
	if len(logs) < 3 {
		t.Fatalf("audit entries: %d", len(logs))
	}
}

func TestTenantIsolation(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	a, b := e.tenant(t), e.tenant(t)

	name := "Secret Customer"
	c, err := e.svc.CreateCustomer(ctx, a, domain.CustomerInput{Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.GetCustomer(ctx, b, c.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("tenant B read tenant A's customer: %v", err)
	}
	// B cannot attach A's customer to its own estimate (composite FK + RLS).
	if _, err := e.svc.CreateDocument(ctx, b, domain.KindEstimate, domain.DocumentInput{CustomerID: &c.ID}); err == nil {
		t.Fatal("tenant B created an estimate for tenant A's customer")
	}
	// Raw SQL without a tenant context sees nothing.
	var n int64
	e.app.Model(&models.Customer{}).Count(&n)
	if n != 0 {
		t.Fatalf("app role without tenant context saw %d customers", n)
	}
	// Raw SQL cannot insert a row for another tenant.
	err = database.WithTenant(ctx, e.app, b.TenantID, func(tx *gorm.DB) error {
		return tx.Exec("INSERT INTO customers (id, tenant_id, name) VALUES (?, ?, 'x')", uuid.New(), a.TenantID).Error
	})
	if err == nil {
		t.Fatal("RLS WITH CHECK should block cross-tenant insert")
	}
}

func TestNumberingIsGaplessAndConcurrent(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	a := e.tenant(t)
	name := "C"
	c, _ := e.svc.CreateCustomer(ctx, a, domain.CustomerInput{Name: &name})
	items := []domain.ItemInput{{Description: "x", Quantity: d("1"), UnitPrice: d("10")}}

	const n = 4
	ids := make([]uuid.UUID, n)
	for i := range ids {
		doc, err := e.svc.CreateDocument(ctx, a, domain.KindInvoice, domain.DocumentInput{CustomerID: &c.ID, Items: &items})
		if err != nil {
			t.Fatal(err)
		}
		ids[i] = doc.ID
	}
	errs := make(chan error, n)
	for _, id := range ids {
		go func(id uuid.UUID) {
			_, err := e.svc.Finalise(ctx, a, domain.KindInvoice, id)
			errs <- err
		}(id)
	}
	for range ids {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	for _, id := range ids {
		v, _ := e.svc.GetDocument(ctx, a, domain.KindInvoice, id)
		if seen[*v.Number] {
			t.Fatalf("duplicate number %s", *v.Number)
		}
		seen[*v.Number] = true
	}
	for i := 1; i <= n; i++ {
		want := "INV-" + models.Today().Format("2006") + "-000" + string(rune('0'+i))
		if !seen[want] {
			t.Fatalf("missing %s in %v", want, seen)
		}
	}
}

func TestTemplateValidation(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	a := e.tenant(t)
	name := "Broken"
	bad := "#let x = data.nope.field"
	if _, err := e.svc.CreateTemplate(ctx, a, domain.TemplateInput{Name: &name, DocType: "estimate", Source: &bad}); err == nil {
		t.Fatal("broken template accepted")
	}
	pkg := `#import "@preview/tablex:0.0.8": tablex`
	if _, err := e.svc.CreateTemplate(ctx, a, domain.TemplateInput{Name: &name, DocType: "estimate", Source: &pkg}); err == nil {
		t.Fatal("package import accepted")
	}
	ok := "Minimal"
	v, err := e.svc.CreateTemplate(ctx, a, domain.TemplateInput{Name: &ok, DocType: "invoice"})
	if err != nil {
		t.Fatal(err)
	}
	src := v.Source + "\n// edit"
	v2, err := e.svc.UpdateTemplate(ctx, a, v.ID, domain.TemplateInput{Source: &src})
	if err != nil || v2.CurrentVersion != 2 {
		t.Fatalf("update: %v %+v", err, v2)
	}
	vers, _ := e.svc.ListTemplateVersions(ctx, a, v.ID)
	v3, err := e.svc.RestoreTemplateVersion(ctx, a, v.ID, vers[len(vers)-1].ID)
	if err != nil || v3.CurrentVersion != 3 {
		t.Fatalf("restore: %v", err)
	}
}

func TestTemplatePreviewUsesRealBusinessAndLogo(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	a := e.tenant(t)

	// A template that only works when the real logo and business name are passed in.
	src := `#let data = json("data.json")
#assert(data.issuer.name == "Test Co", message: "issuer is not the real business: " + data.issuer.name)
#assert(data.issuer.logo != "", message: "logo missing")
#image(data.issuer.logo, height: 1cm)
#data.issuer.name`
	name := "Logo check"
	if _, err := e.svc.CreateTemplate(ctx, a, domain.TemplateInput{Name: &name, DocType: "estimate", Source: &src}); err == nil {
		t.Fatal("expected failure before a logo is uploaded")
	}

	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(1, 1, color.RGBA{240, 90, 40, 255})
	_ = png.Encode(&buf, img)
	if err := e.svc.UploadLogo(ctx, a, buf.Bytes()); err != nil {
		t.Fatal(err)
	}

	tv, err := e.svc.CreateTemplate(ctx, a, domain.TemplateInput{Name: &name, DocType: "estimate", Source: &src})
	if err != nil {
		t.Fatalf("template using the real logo should compile: %v", err)
	}
	links, err := e.svc.TemplatePreviewLinks(ctx, a, tv.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	info, err := e.svc.PreviewInfo(ctx, links.PreviewURL[len("http://localhost:3000/p/"):])
	if err != nil || info.Pages != 1 {
		t.Fatalf("preview: %+v %v", info, err)
	}
}
