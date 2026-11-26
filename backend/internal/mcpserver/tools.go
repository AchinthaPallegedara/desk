package mcpserver

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/shopspring/decimal"

	"github.com/claviq/desk/backend/internal/domain"
	"github.com/claviq/desk/backend/internal/models"
)

// ---------- input types (AI-facing) ----------

type empty struct{}

type idInput struct {
	ID string `json:"id" jsonschema:"the record id"`
}

type docRef struct {
	Kind string `json:"kind" jsonschema:"estimate or invoice"`
	ID   string `json:"id" jsonschema:"the document id"`
}

type customerFields struct {
	Name     *string `json:"name,omitempty" jsonschema:"contact person's full name (required when creating)"`
	Company  *string `json:"company,omitempty"`
	Phone    *string `json:"phone,omitempty"`
	Email    *string `json:"email,omitempty"`
	Address  *string `json:"address,omitempty"`
	TaxRegNo *string `json:"tax_reg_no,omitempty" jsonschema:"customer's VAT/TIN, shown on tax invoices"`
	Notes    *string `json:"notes,omitempty"`
}

type updateCustomerInput struct {
	CustomerID string `json:"customer_id"`
	customerFields
}

type findCustomersInput struct {
	Query           string `json:"query,omitempty" jsonschema:"name, company, phone or email (fuzzy). Empty lists recent customers"`
	IncludeArchived bool   `json:"include_archived,omitempty"`
}

type itemInput struct {
	Description   string  `json:"description"`
	Quantity      float64 `json:"quantity" jsonschema:"e.g. 1, or hours/days"`
	UnitPrice     float64 `json:"unit_price" jsonschema:"price per unit, excluding tax"`
	DiscountType  string  `json:"discount_type,omitempty" jsonschema:"none (default), percent or amount"`
	DiscountValue float64 `json:"discount_value,omitempty" jsonschema:"percent (0-100) or amount, per discount_type"`
	TaxRateID     string  `json:"tax_rate_id,omitempty" jsonschema:"omit to apply the default tax (see get_business_settings)"`
	NoTax         bool    `json:"no_tax,omitempty" jsonschema:"true for a tax-exempt line"`
	IsAddon       bool    `json:"is_addon,omitempty" jsonschema:"estimates only: optional add-on priced separately and NOT included in the total"`
}

type milestoneInput struct {
	Name    string  `json:"name"`
	Percent float64 `json:"percent" jsonschema:"share of the total; all milestones must add up to 100"`
	DueDate string  `json:"due_date,omitempty" jsonschema:"YYYY-MM-DD"`
	Note    string  `json:"note,omitempty"`
}

type docFields struct {
	TemplateID    *string           `json:"template_id,omitempty" jsonschema:"omit for the default template"`
	BankAccountID *string           `json:"bank_account_id,omitempty" jsonschema:"omit for the default bank account"`
	Title         *string           `json:"title,omitempty" jsonschema:"short subject, e.g. 'Website redesign'"`
	IssueDate     *string           `json:"issue_date,omitempty" jsonschema:"YYYY-MM-DD; defaults to the finalise date"`
	Terms         *string           `json:"terms,omitempty" jsonschema:"terms and conditions; defaults to the business default"`
	Notes         *string           `json:"notes,omitempty"`
	Items         *[]itemInput      `json:"items,omitempty" jsonschema:"line items; when given, REPLACES all existing items"`
	Milestones    *[]milestoneInput `json:"milestones,omitempty" jsonschema:"estimates only: payment schedule; when given, REPLACES all milestones"`
}

type createEstimateInput struct {
	CustomerID string  `json:"customer_id"`
	ValidUntil *string `json:"valid_until,omitempty" jsonschema:"YYYY-MM-DD; defaults to issue date + validity days"`
	docFields
}

type updateEstimateInput struct {
	EstimateID string  `json:"estimate_id"`
	CustomerID *string `json:"customer_id,omitempty"`
	ValidUntil *string `json:"valid_until,omitempty"`
	docFields
}

type createInvoiceInput struct {
	CustomerID string  `json:"customer_id"`
	DueDate    *string `json:"due_date,omitempty" jsonschema:"YYYY-MM-DD; defaults to issue date + payment days"`
	docFields
}

type updateInvoiceInput struct {
	InvoiceID  string  `json:"invoice_id"`
	CustomerID *string `json:"customer_id,omitempty"`
	DueDate    *string `json:"due_date,omitempty"`
	docFields
}

type listDocsInput struct {
	Query      string `json:"query,omitempty" jsonschema:"number, title or customer name"`
	Status     string `json:"status,omitempty" jsonschema:"draft, final or superseded (default: draft and final)"`
	CustomerID string `json:"customer_id,omitempty"`
}

type convertInput struct {
	EstimateID   string   `json:"estimate_id"`
	Mode         string   `json:"mode" jsonschema:"full = one invoice for the estimate; milestones = one invoice per milestone"`
	MilestoneIDs []string `json:"milestone_ids,omitempty" jsonschema:"milestones mode: only these (default all unbilled)"`
	AddonItemIDs []string `json:"addon_item_ids,omitempty" jsonschema:"full mode: add-on items the client accepted"`
}

type finaliseInput struct {
	Kind           string `json:"kind" jsonschema:"estimate or invoice"`
	ID             string `json:"id"`
	OwnerConfirmed bool   `json:"owner_confirmed" jsonschema:"must be true, and only after the owner reviewed the preview and explicitly asked to finalise"`
}

type templateCreateInput struct {
	Name        string `json:"name"`
	DocType     string `json:"doc_type" jsonschema:"estimate or invoice"`
	TypstSource string `json:"typst_source,omitempty" jsonschema:"full Typst source; omit to start from the built-in design"`
	IsDefault   bool   `json:"is_default,omitempty"`
}

type templateUpdateInput struct {
	TemplateID  string  `json:"template_id"`
	Name        *string `json:"name,omitempty"`
	TypstSource *string `json:"typst_source,omitempty" jsonschema:"full new Typst source (saved as a new version after it compiles)"`
	IsDefault   *bool   `json:"is_default,omitempty"`
}

type templateVersionInput struct {
	TemplateID string `json:"template_id"`
	VersionID  string `json:"version_id"`
}

type listTemplatesInput struct {
	DocType string `json:"doc_type,omitempty" jsonschema:"estimate or invoice (default both)"`
}

type fontInput struct {
	Family string `json:"family" jsonschema:"Google Fonts family name, e.g. 'Inter' or 'Noto Sans Sinhala'"`
}

// ---------- conversion helpers ----------

func parseID(s, field string) (uuid.UUID, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%s must be an id like 0192f0e4-…", field)
	}
	return id, nil
}

func parseOptID(s *string, field string) (*uuid.UUID, error) {
	if s == nil || *s == "" {
		return nil, nil
	}
	id, err := parseID(*s, field)
	return &id, err
}

func parseOptDate(s *string, field string) (*models.Date, error) {
	if s == nil || *s == "" {
		return nil, nil
	}
	d, err := models.ParseDate(*s)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", field, err)
	}
	return &d, nil
}

func num(f float64) decimal.Decimal { return decimal.NewFromFloat(f) }

func (f docFields) toDomain(in *domain.DocumentInput) error {
	var err error
	if in.TemplateID, err = parseOptID(f.TemplateID, "template_id"); err != nil {
		return err
	}
	if in.BankAccountID, err = parseOptID(f.BankAccountID, "bank_account_id"); err != nil {
		return err
	}
	if in.IssueDate, err = parseOptDate(f.IssueDate, "issue_date"); err != nil {
		return err
	}
	in.Title, in.Terms, in.Notes = f.Title, f.Terms, f.Notes
	if f.Items != nil {
		items := make([]domain.ItemInput, len(*f.Items))
		for i, it := range *f.Items {
			var tid *uuid.UUID
			if it.TaxRateID != "" {
				id, err := parseID(it.TaxRateID, fmt.Sprintf("items[%d].tax_rate_id", i))
				if err != nil {
					return err
				}
				tid = &id
			}
			items[i] = domain.ItemInput{Description: it.Description, Quantity: num(it.Quantity), UnitPrice: num(it.UnitPrice),
				DiscountType: it.DiscountType, DiscountValue: num(it.DiscountValue), TaxRateID: tid, NoTax: it.NoTax, IsAddon: it.IsAddon}
		}
		in.Items = &items
	}
	if f.Milestones != nil {
		ms := make([]domain.MilestoneInput, len(*f.Milestones))
		for i, m := range *f.Milestones {
			due, err := parseOptDate(&m.DueDate, fmt.Sprintf("milestones[%d].due_date", i))
			if err != nil {
				return err
			}
			note := m.Note
			ms[i] = domain.MilestoneInput{Name: m.Name, Percent: num(m.Percent), DueDate: due, Note: &note}
		}
		in.Milestones = &ms
	}
	return nil
}

// docResult is what document tools return: the document plus review links.
type docResult struct {
	Document   *domain.DocumentView `json:"document"`
	PreviewURL string               `json:"preview_url"`
	PDFURL     string               `json:"pdf_url"`
	Next       string               `json:"next,omitempty"`
}

func (s *Server) withLinks(ctx context.Context, a domain.Actor, d *domain.DocumentView, next string) (*docResult, error) {
	l, err := s.svc.DocumentLinks(ctx, a, d.Kind, d.ID)
	if err != nil {
		return nil, err
	}
	return &docResult{Document: d, PreviewURL: l.PreviewURL, PDFURL: l.PDFURL, Next: next}, nil
}

const draftNext = "Share preview_url with the owner for review. Finalise only when they explicitly confirm."

// ---------- registration ----------

func (s *Server) register(srv *mcp.Server) {
	// Settings
	mcp.AddTool(srv, &mcp.Tool{Name: "get_business_settings", Annotations: readOnly("Business settings"),
		Description: "Business details, tax rates (ids, default), bank accounts and numbering. Use tax_rate_id / bank_account_id from here."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ empty) (*mcp.CallToolResult, any, error) {
			a, err := s.actor(ctx)
			if err != nil {
				return nil, nil, err
			}
			st, err := s.svc.GetSettings(ctx, a)
			if err != nil {
				return nil, nil, s.toolErr(err)
			}
			return nil, st, nil
		})

	// Customers
	mcp.AddTool(srv, &mcp.Tool{Name: "find_customers", Annotations: readOnly("Find customers"),
		Description: "Search customers by name, company, phone or email. Always search before creating a customer."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in findCustomersInput) (*mcp.CallToolResult, any, error) {
			a, err := s.actor(ctx)
			if err != nil {
				return nil, nil, err
			}
			cs, err := s.svc.ListCustomers(ctx, a, domain.CustomerFilter{Query: in.Query, IncludeArchived: in.IncludeArchived, Limit: 20})
			if err != nil {
				return nil, nil, s.toolErr(err)
			}
			return nil, map[string]any{"customers": cs, "count": len(cs)}, nil
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "get_customer", Annotations: readOnly("Get customer"),
		Description: "Get one customer by id."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, any, error) {
			a, err := s.actor(ctx)
			if err != nil {
				return nil, nil, err
			}
			id, err := parseID(in.ID, "id")
			if err != nil {
				return nil, nil, err
			}
			c, err := s.svc.GetCustomer(ctx, a, id)
			if err != nil {
				return nil, nil, s.toolErr(err)
			}
			return nil, c, nil
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "create_customer", Annotations: writes("Create customer"),
		Description: "Create a customer. Search with find_customers first to avoid duplicates."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in customerFields) (*mcp.CallToolResult, any, error) {
			a, err := s.actor(ctx)
			if err != nil {
				return nil, nil, err
			}
			c, err := s.svc.CreateCustomer(ctx, a, domain.CustomerInput(in))
			if err != nil {
				return nil, nil, s.toolErr(err)
			}
			return nil, c, nil
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "update_customer", Annotations: writes("Update customer"),
		Description: "Update customer fields; omitted fields are unchanged, empty string clears a field."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in updateCustomerInput) (*mcp.CallToolResult, any, error) {
			a, err := s.actor(ctx)
			if err != nil {
				return nil, nil, err
			}
			id, err := parseID(in.CustomerID, "customer_id")
			if err != nil {
				return nil, nil, err
			}
			c, err := s.svc.UpdateCustomer(ctx, a, id, domain.CustomerInput(in.customerFields))
			if err != nil {
				return nil, nil, s.toolErr(err)
			}
			return nil, c, nil
		})

	// Estimates
	mcp.AddTool(srv, &mcp.Tool{Name: "create_estimate", Annotations: writes("Create estimate"),
		Description: "Create a DRAFT estimate for a customer from a template (default if omitted). Returns totals and a preview_url for the owner."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in createEstimateInput) (*mcp.CallToolResult, any, error) {
			a, err := s.actor(ctx)
			if err != nil {
				return nil, nil, err
			}
			var di domain.DocumentInput
			cid, err := parseID(in.CustomerID, "customer_id")
			if err != nil {
				return nil, nil, err
			}
			di.CustomerID = &cid
			if di.ValidUntil, err = parseOptDate(in.ValidUntil, "valid_until"); err != nil {
				return nil, nil, err
			}
			if err := in.docFields.toDomain(&di); err != nil {
				return nil, nil, err
			}
			d, err := s.svc.CreateDocument(ctx, a, domain.KindEstimate, di)
			if err != nil {
				return nil, nil, s.toolErr(err)
			}
			res, err := s.withLinks(ctx, a, d, draftNext)
			if err != nil {
				return nil, nil, s.toolErr(err)
			}
			return nil, res, nil
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "update_estimate", Annotations: writes("Update estimate"),
		Description: "Edit a DRAFT estimate. Only given fields change; items/milestones, when given, replace the whole list (send the complete list). Returns a fresh preview_url."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in updateEstimateInput) (*mcp.CallToolResult, any, error) {
			a, err := s.actor(ctx)
			if err != nil {
				return nil, nil, err
			}
			id, err := parseID(in.EstimateID, "estimate_id")
			if err != nil {
				return nil, nil, err
			}
			var di domain.DocumentInput
			if di.CustomerID, err = parseOptID(in.CustomerID, "customer_id"); err != nil {
				return nil, nil, err
			}
			if di.ValidUntil, err = parseOptDate(in.ValidUntil, "valid_until"); err != nil {
				return nil, nil, err
			}
			if err := in.docFields.toDomain(&di); err != nil {
				return nil, nil, err
			}
			d, err := s.svc.UpdateDocument(ctx, a, domain.KindEstimate, id, di)
			if err != nil {
				return nil, nil, s.toolErr(err)
			}
			res, err := s.withLinks(ctx, a, d, draftNext)
			if err != nil {
				return nil, nil, s.toolErr(err)
			}
			return nil, res, nil
		})

	// Invoices
	mcp.AddTool(srv, &mcp.Tool{Name: "create_invoice", Annotations: writes("Create invoice"),
		Description: "Create a DRAFT invoice directly (to invoice from an estimate, use convert_estimate_to_invoices). Returns a preview_url."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in createInvoiceInput) (*mcp.CallToolResult, any, error) {
			a, err := s.actor(ctx)
			if err != nil {
				return nil, nil, err
			}
			var di domain.DocumentInput
			cid, err := parseID(in.CustomerID, "customer_id")
			if err != nil {
				return nil, nil, err
			}
			di.CustomerID = &cid
			if di.DueDate, err = parseOptDate(in.DueDate, "due_date"); err != nil {
				return nil, nil, err
			}
			if err := in.docFields.toDomain(&di); err != nil {
				return nil, nil, err
			}
			d, err := s.svc.CreateDocument(ctx, a, domain.KindInvoice, di)
			if err != nil {
				return nil, nil, s.toolErr(err)
			}
			res, err := s.withLinks(ctx, a, d, draftNext)
			if err != nil {
				return nil, nil, s.toolErr(err)
			}
			return nil, res, nil
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "update_invoice", Annotations: writes("Update invoice"),
		Description: "Edit a DRAFT invoice. Only given fields change; items, when given, replace the whole list."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in updateInvoiceInput) (*mcp.CallToolResult, any, error) {
			a, err := s.actor(ctx)
			if err != nil {
				return nil, nil, err
			}
			id, err := parseID(in.InvoiceID, "invoice_id")
			if err != nil {
				return nil, nil, err
			}
			var di domain.DocumentInput
			if di.CustomerID, err = parseOptID(in.CustomerID, "customer_id"); err != nil {
				return nil, nil, err
			}
			if di.DueDate, err = parseOptDate(in.DueDate, "due_date"); err != nil {
				return nil, nil, err
			}
			if err := in.docFields.toDomain(&di); err != nil {
				return nil, nil, err
			}
			d, err := s.svc.UpdateDocument(ctx, a, domain.KindInvoice, id, di)
			if err != nil {
				return nil, nil, s.toolErr(err)
			}
			res, err := s.withLinks(ctx, a, d, draftNext)
			if err != nil {
				return nil, nil, s.toolErr(err)
			}
			return nil, res, nil
		})

	// Shared document tools
	for _, kind := range []string{domain.KindEstimate, domain.KindInvoice} {
		kind := kind
		mcp.AddTool(srv, &mcp.Tool{Name: "list_" + kind + "s", Annotations: readOnly("List " + kind + "s"),
			Description: "List or search " + kind + "s (newest first). Superseded revisions are hidden unless status=superseded."},
			func(ctx context.Context, _ *mcp.CallToolRequest, in listDocsInput) (*mcp.CallToolResult, any, error) {
				a, err := s.actor(ctx)
				if err != nil {
					return nil, nil, err
				}
				f := domain.DocumentFilter{Query: in.Query, Status: trimLower(in.Status), Limit: 25}
				if f.CustomerID, err = parseOptID(&in.CustomerID, "customer_id"); err != nil {
					return nil, nil, err
				}
				docs, err := s.svc.ListDocuments(ctx, a, kind, f)
				if err != nil {
					return nil, nil, s.toolErr(err)
				}
				return nil, map[string]any{kind + "s": docs, "count": len(docs)}, nil
			})

		mcp.AddTool(srv, &mcp.Tool{Name: "get_" + kind, Annotations: readOnly("Get " + kind),
			Description: "Get a full " + kind + " (items, totals" + map[bool]string{true: ", milestones, linked invoices", false: ""}[kind == domain.KindEstimate] + ") with preview and PDF links."},
			func(ctx context.Context, _ *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, any, error) {
				a, err := s.actor(ctx)
				if err != nil {
					return nil, nil, err
				}
				id, err := parseID(in.ID, "id")
				if err != nil {
					return nil, nil, err
				}
				d, err := s.svc.GetDocument(ctx, a, kind, id)
				if err != nil {
					return nil, nil, s.toolErr(err)
				}
				res, err := s.withLinks(ctx, a, d, "")
				if err != nil {
					return nil, nil, s.toolErr(err)
				}
				return nil, res, nil
			})
	}

	mcp.AddTool(srv, &mcp.Tool{Name: "get_preview_link", Annotations: readOnly("Get preview link"),
		Description: "Signed preview link (opens on a phone without login, expires in 7 days) for an estimate or invoice."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in docRef) (*mcp.CallToolResult, any, error) {
			return s.links(ctx, in, false)
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "get_pdf_link", Annotations: readOnly("Get PDF link"),
		Description: "Signed PDF download link. Final documents return the stored PDF; drafts are watermarked DRAFT."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in docRef) (*mcp.CallToolResult, any, error) {
			return s.links(ctx, in, true)
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "finalise_document", Annotations: destructive("Finalise document"),
		Description: "Issue a draft: assigns the number, locks it and stores the PDF. IRREVERSIBLE. Only call after the owner reviewed the preview and explicitly said to finalise; set owner_confirmed=true."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in finaliseInput) (*mcp.CallToolResult, any, error) {
			a, err := s.actor(ctx)
			if err != nil {
				return nil, nil, err
			}
			if !in.OwnerConfirmed {
				return nil, nil, fmt.Errorf("not finalised: ask the owner to review the preview and confirm, then call again with owner_confirmed=true")
			}
			id, err := parseID(in.ID, "id")
			if err != nil {
				return nil, nil, err
			}
			d, err := s.svc.Finalise(ctx, a, trimLower(in.Kind), id)
			if err != nil {
				return nil, nil, s.toolErr(err)
			}
			res, err := s.withLinks(ctx, a, d, "Give the owner pdf_url to download and send manually.")
			if err != nil {
				return nil, nil, s.toolErr(err)
			}
			return nil, res, nil
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "revise_document", Annotations: writes("Revise document"),
		Description: "Create a new DRAFT revision of a FINAL estimate or invoice (same number, revision+1). Edit it, then finalise to replace the original."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in docRef) (*mcp.CallToolResult, any, error) {
			a, err := s.actor(ctx)
			if err != nil {
				return nil, nil, err
			}
			id, err := parseID(in.ID, "id")
			if err != nil {
				return nil, nil, err
			}
			d, err := s.svc.Revise(ctx, a, trimLower(in.Kind), id)
			if err != nil {
				return nil, nil, s.toolErr(err)
			}
			res, err := s.withLinks(ctx, a, d, draftNext)
			if err != nil {
				return nil, nil, s.toolErr(err)
			}
			return nil, res, nil
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "delete_draft", Annotations: destructive("Delete draft"),
		Description: "Delete a DRAFT estimate or invoice. Final documents cannot be deleted. Confirm with the owner first."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in docRef) (*mcp.CallToolResult, any, error) {
			a, err := s.actor(ctx)
			if err != nil {
				return nil, nil, err
			}
			id, err := parseID(in.ID, "id")
			if err != nil {
				return nil, nil, err
			}
			if err := s.svc.DeleteDraft(ctx, a, trimLower(in.Kind), id); err != nil {
				return nil, nil, s.toolErr(err)
			}
			return nil, map[string]string{"status": "deleted"}, nil
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "convert_estimate_to_invoices", Annotations: writes("Convert estimate to invoice"),
		Description: "From a FINAL estimate, create draft invoice(s): mode=full (one invoice; accepted add-ons optional) or mode=milestones (one invoice per milestone)."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in convertInput) (*mcp.CallToolResult, any, error) {
			a, err := s.actor(ctx)
			if err != nil {
				return nil, nil, err
			}
			id, err := parseID(in.EstimateID, "estimate_id")
			if err != nil {
				return nil, nil, err
			}
			ci := domain.ConvertInput{Mode: trimLower(in.Mode)}
			for _, m := range in.MilestoneIDs {
				mid, err := parseID(m, "milestone_ids")
				if err != nil {
					return nil, nil, err
				}
				ci.MilestoneIDs = append(ci.MilestoneIDs, mid)
			}
			for _, x := range in.AddonItemIDs {
				xid, err := parseID(x, "addon_item_ids")
				if err != nil {
					return nil, nil, err
				}
				ci.AddonItemIDs = append(ci.AddonItemIDs, xid)
			}
			docs, err := s.svc.ConvertToInvoices(ctx, a, id, ci)
			if err != nil {
				return nil, nil, s.toolErr(err)
			}
			out := make([]*docResult, 0, len(docs))
			for i := range docs {
				r, err := s.withLinks(ctx, a, &docs[i], "")
				if err != nil {
					return nil, nil, s.toolErr(err)
				}
				out = append(out, r)
			}
			return nil, map[string]any{"invoices": out, "next": draftNext}, nil
		})

	s.registerTemplateTools(srv)
}

func (s *Server) links(ctx context.Context, in docRef, pdf bool) (*mcp.CallToolResult, any, error) {
	a, err := s.actor(ctx)
	if err != nil {
		return nil, nil, err
	}
	id, err := parseID(in.ID, "id")
	if err != nil {
		return nil, nil, err
	}
	l, err := s.svc.DocumentLinks(ctx, a, trimLower(in.Kind), id)
	if err != nil {
		return nil, nil, s.toolErr(err)
	}
	if pdf {
		return nil, map[string]any{"pdf_url": l.PDFURL, "expires_at": l.ExpiresAt}, nil
	}
	return nil, map[string]any{"preview_url": l.PreviewURL, "expires_at": l.ExpiresAt}, nil
}
