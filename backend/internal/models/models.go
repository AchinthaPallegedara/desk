package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// ---------- tenant & configuration ----------

type Tenant struct {
	ID                  uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	Name                string    `json:"name"`
	LegalName           *string   `json:"legal_name"`
	RegNo               *string   `json:"reg_no"`
	TaxRegNo            *string   `json:"tax_reg_no"`
	Address             *string   `json:"address"`
	Phone               *string   `json:"phone"`
	Email               *string   `json:"email"`
	LogoPath            *string   `json:"-"`
	Currency            string    `json:"currency"`
	DefaultTerms        *string   `json:"default_terms"`
	DefaultValidityDays int       `json:"default_validity_days"`
	DefaultPaymentDays  int       `json:"default_payment_days"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type User struct {
	ID           uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`
	TenantID     uuid.UUID  `gorm:"type:uuid" json:"tenant_id"`
	Email        string     `json:"email"`
	PasswordHash string     `json:"-"`
	Role         string     `json:"role"`
	LastLoginAt  *time.Time `json:"last_login_at"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type TaxRate struct {
	ID        uuid.UUID       `gorm:"type:uuid;primaryKey" json:"id"`
	TenantID  uuid.UUID       `gorm:"type:uuid" json:"-"`
	Name      string          `json:"name"`
	Rate      decimal.Decimal `gorm:"type:numeric" json:"rate"`
	IsDefault bool            `json:"is_default"`
	Active    bool            `json:"active"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

type BankAccount struct {
	ID            uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	TenantID      uuid.UUID `gorm:"type:uuid" json:"-"`
	AccountName   string    `json:"account_name"`
	AccountNumber string    `json:"account_number"`
	Bank          string    `json:"bank"`
	Branch        *string   `json:"branch"`
	Swift         *string   `json:"swift"`
	IsDefault     bool      `json:"is_default"`
	Active        bool      `json:"active"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type NumberSequence struct {
	TenantID      uuid.UUID `gorm:"type:uuid;primaryKey" json:"-"`
	DocType       string    `gorm:"primaryKey" json:"doc_type"`
	Prefix        string    `json:"prefix"`
	Format        string    `json:"format"`
	ResetPolicy   string    `json:"reset_policy"`
	CurrentPeriod *int      `json:"current_period"`
	NextValue     int       `json:"next_value"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// ---------- oauth ----------

type OAuthClient struct {
	ID                      uuid.UUID       `gorm:"type:uuid;primaryKey"`
	ClientID                string          `gorm:"column:client_id"`
	ClientSecretHash        *string
	Name                    string
	RedirectURIs            JSONB[[]string] `gorm:"column:redirect_uris;type:jsonb"`
	GrantTypes              JSONB[[]string] `gorm:"type:jsonb"`
	ResponseTypes           JSONB[[]string] `gorm:"type:jsonb"`
	Scopes                  JSONB[[]string] `gorm:"type:jsonb"`
	TokenEndpointAuthMethod string
	RegisteredVia           string
	CreatedAt               time.Time
	LastUsedAt              *time.Time
}

func (OAuthClient) TableName() string { return "oauth_clients" }

type OAuthToken struct {
	ID              uuid.UUID       `gorm:"type:uuid;primaryKey"`
	Kind            string
	SignatureHash   string
	RequestID       string
	ClientID        uuid.UUID       `gorm:"type:uuid;column:client_id"`
	TenantID        uuid.UUID       `gorm:"type:uuid"`
	UserID          uuid.UUID       `gorm:"type:uuid"`
	Scopes          JSONB[[]string] `gorm:"type:jsonb"`
	GrantedAudience JSONB[[]string] `gorm:"type:jsonb"`
	SessionData     []byte          `gorm:"type:jsonb"`
	ExpiresAt       *time.Time
	UsedAt          *time.Time
	RevokedAt       *time.Time
	CreatedAt       time.Time
}

func (OAuthToken) TableName() string { return "oauth_tokens" }

// ---------- customers ----------

type Customer struct {
	ID         uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`
	TenantID   uuid.UUID  `gorm:"type:uuid" json:"-"`
	Name       string     `json:"name"`
	Company    *string    `json:"company"`
	Phone      *string    `json:"phone"`
	Email      *string    `json:"email"`
	Address    *string    `json:"address"`
	TaxRegNo   *string    `json:"tax_reg_no"`
	Notes      *string    `json:"notes"`
	ArchivedAt *time.Time `json:"archived_at"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// ---------- templates & fonts ----------

type Template struct {
	ID               uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`
	TenantID         uuid.UUID  `gorm:"type:uuid" json:"-"`
	Name             string     `json:"name"`
	DocType          string     `json:"doc_type"`
	CurrentVersionID *uuid.UUID `gorm:"type:uuid" json:"current_version_id"`
	IsDefault        bool       `json:"is_default"`
	ArchivedAt       *time.Time `json:"archived_at"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type TemplateVersion struct {
	ID              uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`
	TenantID        uuid.UUID  `gorm:"type:uuid" json:"-"`
	TemplateID      uuid.UUID  `gorm:"type:uuid" json:"template_id"`
	DocType         string     `json:"doc_type"`
	Version         int        `json:"version"`
	TypstSource     string     `json:"typst_source,omitempty"`
	CreatedByUserID *uuid.UUID `gorm:"type:uuid" json:"created_by_user_id"`
	CreatedVia      string     `json:"created_via"`
	CreatedAt       time.Time  `json:"created_at"`
}

type Font struct {
	ID        uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`
	Family    string     `json:"family"`
	TenantID  *uuid.UUID `gorm:"type:uuid" json:"-"`
	Source    string     `json:"source"`
	Licence   *string    `json:"licence"`
	CreatedAt time.Time  `json:"created_at"`
	Files     []FontFile `gorm:"foreignKey:FontID" json:"files,omitempty"`
}

type FontFile struct {
	ID        uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`
	FontID    uuid.UUID  `gorm:"type:uuid" json:"-"`
	TenantID  *uuid.UUID `gorm:"type:uuid" json:"-"`
	Weight    int        `json:"weight"`
	Style     string     `json:"style"`
	FilePath  string     `json:"-"`
	CreatedAt time.Time  `json:"-"`
}

// ---------- documents ----------

// CustomerSnapshot / IssuerSnapshot are frozen into a document at finalisation.
type CustomerSnapshot struct {
	Name     string  `json:"name"`
	Company  *string `json:"company,omitempty"`
	Address  *string `json:"address,omitempty"`
	Phone    *string `json:"phone,omitempty"`
	Email    *string `json:"email,omitempty"`
	TaxRegNo *string `json:"tax_reg_no,omitempty"`
}

type BankSnapshot struct {
	AccountName   string  `json:"account_name"`
	AccountNumber string  `json:"account_number"`
	Bank          string  `json:"bank"`
	Branch        *string `json:"branch,omitempty"`
	Swift         *string `json:"swift,omitempty"`
}

type IssuerSnapshot struct {
	Name      string        `json:"name"`
	LegalName *string       `json:"legal_name,omitempty"`
	RegNo     *string       `json:"reg_no,omitempty"`
	TaxRegNo  *string       `json:"tax_reg_no,omitempty"`
	Address   *string       `json:"address,omitempty"`
	Phone     *string       `json:"phone,omitempty"`
	Email     *string       `json:"email,omitempty"`
	LogoPath  *string       `json:"logo_path,omitempty"`
	Bank      *BankSnapshot `json:"bank,omitempty"`
}

// DocumentHeader holds the columns shared by estimates and invoices.
type DocumentHeader struct {
	ID                 uuid.UUID                `gorm:"type:uuid;primaryKey" json:"id"`
	TenantID           uuid.UUID                `gorm:"type:uuid" json:"-"`
	CustomerID         uuid.UUID                `gorm:"type:uuid" json:"customer_id"`
	TemplateVersionID  uuid.UUID                `gorm:"type:uuid" json:"template_version_id"`
	DocType            string                   `gorm:"->" json:"doc_type"` // read-only: DB default
	BankAccountID      *uuid.UUID               `gorm:"type:uuid" json:"bank_account_id"`
	Number             *string                  `json:"number"`
	Revision           int                      `json:"revision"`
	PreviousRevisionID *uuid.UUID               `gorm:"type:uuid" json:"previous_revision_id"`
	Status             string                   `json:"status"`
	Title              *string                  `json:"title"`
	Currency           string                   `json:"currency"`
	IssueDate          *Date                    `gorm:"type:date" json:"issue_date"`
	Terms              *string                  `json:"terms"`
	Notes              *string                  `json:"notes"`
	Subtotal           decimal.NullDecimal      `gorm:"type:numeric" json:"-"`
	DiscountTotal      decimal.NullDecimal      `gorm:"type:numeric" json:"-"`
	TaxTotal           decimal.NullDecimal      `gorm:"type:numeric" json:"-"`
	Total              decimal.NullDecimal      `gorm:"type:numeric" json:"-"`
	CustomerSnapshot   *JSONB[CustomerSnapshot] `gorm:"type:jsonb" json:"-"`
	IssuerSnapshot     *JSONB[IssuerSnapshot]   `gorm:"type:jsonb" json:"-"`
	PDFPath            *string                  `gorm:"column:pdf_path" json:"-"`
	PDFSHA256          *string                  `gorm:"column:pdf_sha256" json:"-"`
	FinalisedAt        *time.Time               `json:"finalised_at"`
	FinalisedBy        *uuid.UUID               `gorm:"type:uuid" json:"-"`
	CreatedAt          time.Time                `json:"created_at"`
	UpdatedAt          time.Time                `json:"updated_at"`
}

// LineItem holds the columns shared by estimate_items and invoice_items.
type LineItem struct {
	ID            uuid.UUID           `gorm:"type:uuid;primaryKey" json:"id"`
	TenantID      uuid.UUID           `gorm:"type:uuid" json:"-"`
	SortOrder     int                 `json:"sort_order"`
	Description   string              `json:"description"`
	Quantity      decimal.Decimal     `gorm:"type:numeric" json:"quantity"`
	UnitPrice     decimal.Decimal     `gorm:"type:numeric" json:"unit_price"`
	DiscountType  string              `json:"discount_type"`
	DiscountValue decimal.Decimal     `gorm:"type:numeric" json:"discount_value"`
	TaxRateID     *uuid.UUID          `gorm:"type:uuid" json:"tax_rate_id"`
	TaxName       *string             `json:"tax_name"`
	TaxRate       decimal.NullDecimal `gorm:"type:numeric" json:"-"`
	LineTotal     decimal.NullDecimal `gorm:"type:numeric" json:"-"`
}

type Estimate struct {
	DocumentHeader `gorm:"embedded"`
	ValidUntil     *Date          `gorm:"type:date" json:"valid_until"`
	Items          []EstimateItem `gorm:"foreignKey:EstimateID" json:"items"`
	Milestones     []Milestone    `gorm:"foreignKey:EstimateID" json:"milestones"`
}

type EstimateItem struct {
	LineItem   `gorm:"embedded"`
	EstimateID uuid.UUID `gorm:"type:uuid" json:"-"`
	IsAddon    bool      `json:"is_addon"`
}

type Milestone struct {
	ID          uuid.UUID       `gorm:"type:uuid;primaryKey" json:"id"`
	TenantID    uuid.UUID       `gorm:"type:uuid" json:"-"`
	EstimateID  uuid.UUID       `gorm:"type:uuid" json:"-"`
	SortOrder   int             `json:"sort_order"`
	Name        string          `json:"name"`
	Note        *string         `json:"note"`
	Percent     decimal.Decimal `gorm:"type:numeric" json:"percent"`
	DueDate     *Date           `gorm:"type:date" json:"due_date"`
	InvoiceID   *uuid.UUID      `gorm:"type:uuid" json:"invoice_id"`
}

type Invoice struct {
	DocumentHeader `gorm:"embedded"`
	EstimateID     *uuid.UUID    `gorm:"type:uuid" json:"estimate_id"`
	DueDate        *Date         `gorm:"type:date" json:"due_date"`
	Items          []InvoiceItem `gorm:"foreignKey:InvoiceID" json:"items"`
}

type InvoiceItem struct {
	LineItem  `gorm:"embedded"`
	InvoiceID uuid.UUID `gorm:"type:uuid" json:"-"`
}

// ---------- audit ----------

type AuditLog struct {
	ID            int64                `gorm:"primaryKey;->" json:"id"`
	TenantID      uuid.UUID            `gorm:"type:uuid" json:"-"`
	UserID        *uuid.UUID           `gorm:"type:uuid" json:"user_id"`
	Via           string               `json:"via"`
	OAuthClientID *uuid.UUID           `gorm:"type:uuid;column:oauth_client_id" json:"oauth_client_id"`
	RequestID     *string              `json:"request_id"`
	Action        string               `json:"action"`
	Entity        string               `json:"entity"`
	EntityID      *string              `json:"entity_id"`
	Changes       *JSONB[any]          `gorm:"type:jsonb" json:"changes"`
	CreatedAt     time.Time            `gorm:"->;<-:false" json:"created_at"`
}
