-- +goose Up
-- Phase 1 schema: 18 tables. See docs/data-model.md for the rationale of every rule.

-- ============================================================
-- Tenants and configuration
-- ============================================================

CREATE TABLE tenants (
  id                    uuid PRIMARY KEY,
  name                  text NOT NULL CHECK (name <> ''),
  legal_name            text,
  reg_no                text,
  tax_reg_no            text,
  address               text,
  phone                 text,
  email                 citext,
  logo_path             text,
  currency              char(3) NOT NULL DEFAULT 'LKR',
  default_terms         text,
  default_validity_days int NOT NULL DEFAULT 30 CHECK (default_validity_days BETWEEN 1 AND 365),
  default_payment_days  int NOT NULL DEFAULT 14 CHECK (default_payment_days BETWEEN 0 AND 365),
  created_at            timestamptz NOT NULL DEFAULT now(),
  updated_at            timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE users (
  id            uuid PRIMARY KEY,
  tenant_id     uuid NOT NULL REFERENCES tenants (id),
  email         citext NOT NULL UNIQUE,
  password_hash text NOT NULL,
  role          text NOT NULL DEFAULT 'owner' CHECK (role IN ('owner')),
  last_login_at timestamptz,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, id)
);

CREATE TABLE tax_rates (
  id         uuid PRIMARY KEY,
  tenant_id  uuid NOT NULL REFERENCES tenants (id),
  name       text NOT NULL CHECK (name <> ''),
  rate       numeric(6,3) NOT NULL CHECK (rate >= 0 AND rate <= 100),
  is_default boolean NOT NULL DEFAULT false,
  active     boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, id),
  UNIQUE (tenant_id, name)
);
CREATE UNIQUE INDEX tax_rates_one_default ON tax_rates (tenant_id) WHERE is_default;

CREATE TABLE bank_accounts (
  id             uuid PRIMARY KEY,
  tenant_id      uuid NOT NULL REFERENCES tenants (id),
  account_name   text NOT NULL,
  account_number text NOT NULL,
  bank           text NOT NULL,
  branch         text,
  swift          text,
  is_default     boolean NOT NULL DEFAULT false,
  active         boolean NOT NULL DEFAULT true,
  created_at     timestamptz NOT NULL DEFAULT now(),
  updated_at     timestamptz NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, id)
);
CREATE UNIQUE INDEX bank_accounts_one_default ON bank_accounts (tenant_id) WHERE is_default;

CREATE TABLE number_sequences (
  tenant_id      uuid NOT NULL REFERENCES tenants (id),
  doc_type       text NOT NULL CHECK (doc_type IN ('estimate','invoice')),
  prefix         text NOT NULL,
  format         text NOT NULL DEFAULT '{prefix}-{yyyy}-{seq:4}',
  reset_policy   text NOT NULL DEFAULT 'yearly' CHECK (reset_policy IN ('never','yearly')),
  current_period int,
  next_value     int NOT NULL DEFAULT 1 CHECK (next_value >= 1),
  updated_at     timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, doc_type)
);

-- ============================================================
-- OAuth (global / pre-tenant; no RLS — accessed only by internal/auth)
-- ============================================================

CREATE TABLE oauth_clients (
  id                         uuid PRIMARY KEY,
  client_id                  text NOT NULL UNIQUE,
  client_secret_hash         text,
  name                       text NOT NULL,
  redirect_uris              jsonb NOT NULL,
  grant_types                jsonb NOT NULL,
  response_types             jsonb NOT NULL,
  scopes                     jsonb NOT NULL,
  token_endpoint_auth_method text NOT NULL DEFAULT 'none',
  registered_via             text NOT NULL DEFAULT 'dynamic' CHECK (registered_via IN ('dynamic','manual')),
  created_at                 timestamptz NOT NULL DEFAULT now(),
  last_used_at               timestamptz
);

CREATE TABLE oauth_tokens (
  id               uuid PRIMARY KEY,
  kind             text NOT NULL CHECK (kind IN ('authorization_code','access_token','refresh_token','pkce')),
  signature_hash   text NOT NULL,
  request_id       text NOT NULL,
  client_id        uuid NOT NULL REFERENCES oauth_clients (id) ON DELETE CASCADE,
  tenant_id        uuid NOT NULL,
  user_id          uuid NOT NULL,
  scopes           jsonb NOT NULL DEFAULT '[]',
  granted_audience jsonb NOT NULL DEFAULT '[]',
  session_data     jsonb NOT NULL,
  expires_at       timestamptz,
  used_at          timestamptz,
  revoked_at       timestamptz,
  created_at       timestamptz NOT NULL DEFAULT now(),
  UNIQUE (kind, signature_hash),
  FOREIGN KEY (tenant_id, user_id) REFERENCES users (tenant_id, id) ON DELETE CASCADE
);
CREATE INDEX oauth_tokens_request ON oauth_tokens (request_id);
CREATE INDEX oauth_tokens_expires ON oauth_tokens (expires_at);

-- ============================================================
-- Customers
-- ============================================================

CREATE TABLE customers (
  id          uuid PRIMARY KEY,
  tenant_id   uuid NOT NULL REFERENCES tenants (id),
  name        text NOT NULL CHECK (name <> ''),
  company     text,
  phone       text,
  email       citext,
  address     text,
  tax_reg_no  text,
  notes       text,
  archived_at timestamptz,
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, id)
);
CREATE INDEX customers_search ON customers USING gin (
  (lower(name || ' ' || coalesce(company,'') || ' ' || coalesce(email::text,'') || ' ' || coalesce(phone,''))) gin_trgm_ops
);

-- ============================================================
-- Templates and fonts
-- ============================================================

CREATE TABLE templates (
  id                 uuid PRIMARY KEY,
  tenant_id          uuid NOT NULL REFERENCES tenants (id),
  name               text NOT NULL CHECK (name <> ''),
  doc_type           text NOT NULL CHECK (doc_type IN ('estimate','invoice')),
  current_version_id uuid,
  is_default         boolean NOT NULL DEFAULT false,
  archived_at        timestamptz,
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, id),
  UNIQUE (tenant_id, id, doc_type)
);
CREATE UNIQUE INDEX templates_one_default ON templates (tenant_id, doc_type) WHERE is_default;
CREATE UNIQUE INDEX templates_name ON templates (tenant_id, doc_type, lower(name)) WHERE archived_at IS NULL;

CREATE TABLE template_versions (
  id                 uuid PRIMARY KEY,
  tenant_id          uuid NOT NULL,
  template_id        uuid NOT NULL,
  doc_type           text NOT NULL CHECK (doc_type IN ('estimate','invoice')),
  version            int NOT NULL CHECK (version >= 1),
  typst_source       text NOT NULL,
  created_by_user_id uuid,
  created_via        text NOT NULL CHECK (created_via IN ('web','ai','system')),
  created_at         timestamptz NOT NULL DEFAULT now(),
  UNIQUE (template_id, version),
  UNIQUE (tenant_id, id),
  UNIQUE (tenant_id, id, doc_type),
  UNIQUE (tenant_id, template_id, id),
  FOREIGN KEY (tenant_id, template_id, doc_type) REFERENCES templates (tenant_id, id, doc_type)
);

ALTER TABLE templates
  ADD CONSTRAINT templates_current_version_fk
  FOREIGN KEY (tenant_id, id, current_version_id)
  REFERENCES template_versions (tenant_id, template_id, id)
  DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE fonts (
  id         uuid PRIMARY KEY,
  family     text NOT NULL CHECK (family <> ''),
  tenant_id  uuid REFERENCES tenants (id),
  source     text NOT NULL CHECK (source IN ('bundled','google','upload')),
  licence    text,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX fonts_shared_family  ON fonts (lower(family)) WHERE tenant_id IS NULL;
CREATE UNIQUE INDEX fonts_private_family ON fonts (tenant_id, lower(family)) WHERE tenant_id IS NOT NULL;

CREATE TABLE font_files (
  id         uuid PRIMARY KEY,
  font_id    uuid NOT NULL REFERENCES fonts (id) ON DELETE CASCADE,
  tenant_id  uuid,
  weight     int NOT NULL CHECK (weight BETWEEN 100 AND 900),
  style      text NOT NULL CHECK (style IN ('normal','italic')),
  file_path  text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (font_id, weight, style)
);

-- font_files.tenant_id always mirrors its font, so a private font's file can never become shared.
-- +goose StatementBegin
CREATE FUNCTION font_files_copy_tenant() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  SELECT tenant_id INTO NEW.tenant_id FROM fonts WHERE id = NEW.font_id;
  RETURN NEW;
END
$$;
-- +goose StatementEnd
CREATE TRIGGER font_files_copy_tenant BEFORE INSERT OR UPDATE ON font_files
  FOR EACH ROW EXECUTE FUNCTION font_files_copy_tenant();

-- ============================================================
-- Estimates
-- ============================================================

CREATE TABLE estimates (
  id                   uuid PRIMARY KEY,
  tenant_id            uuid NOT NULL REFERENCES tenants (id),
  customer_id          uuid NOT NULL,
  template_version_id  uuid NOT NULL,
  doc_type             text NOT NULL DEFAULT 'estimate' CHECK (doc_type = 'estimate'),
  bank_account_id      uuid,
  number               text,
  revision             int NOT NULL DEFAULT 0 CHECK (revision >= 0),
  previous_revision_id uuid UNIQUE,
  status               text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','final','superseded')),
  title                text,
  currency             char(3) NOT NULL,
  issue_date           date,
  valid_until          date,
  terms                text,
  notes                text,
  subtotal             numeric(14,2),
  discount_total       numeric(14,2),
  tax_total            numeric(14,2),
  total                numeric(14,2),
  customer_snapshot    jsonb,
  issuer_snapshot      jsonb,
  pdf_path             text,
  pdf_sha256           text,
  finalised_at         timestamptz,
  finalised_by         uuid,
  created_at           timestamptz NOT NULL DEFAULT now(),
  updated_at           timestamptz NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, id),
  UNIQUE (tenant_id, id, customer_id),
  UNIQUE (tenant_id, number, revision),
  FOREIGN KEY (tenant_id, customer_id) REFERENCES customers (tenant_id, id),
  FOREIGN KEY (tenant_id, template_version_id, doc_type) REFERENCES template_versions (tenant_id, id, doc_type),
  FOREIGN KEY (tenant_id, bank_account_id) REFERENCES bank_accounts (tenant_id, id),
  FOREIGN KEY (tenant_id, previous_revision_id) REFERENCES estimates (tenant_id, id),
  FOREIGN KEY (tenant_id, finalised_by) REFERENCES users (tenant_id, id),
  CHECK (status = 'draft' OR number IS NOT NULL),
  CHECK (status = 'draft' OR (total IS NOT NULL AND pdf_path IS NOT NULL AND finalised_at IS NOT NULL
                              AND customer_snapshot IS NOT NULL AND issuer_snapshot IS NOT NULL)),
  CHECK (valid_until IS NULL OR issue_date IS NULL OR valid_until >= issue_date)
);
CREATE UNIQUE INDEX estimates_one_final_per_number ON estimates (tenant_id, number) WHERE status = 'final';
CREATE INDEX estimates_customer ON estimates (tenant_id, customer_id);
CREATE INDEX estimates_created ON estimates (tenant_id, created_at DESC);

CREATE TABLE estimate_items (
  id             uuid PRIMARY KEY,
  tenant_id      uuid NOT NULL,
  estimate_id    uuid NOT NULL,
  sort_order     int NOT NULL,
  description    text NOT NULL CHECK (description <> ''),
  quantity       numeric(12,3) NOT NULL CHECK (quantity > 0),
  unit_price     numeric(14,2) NOT NULL CHECK (unit_price >= 0),
  discount_type  text NOT NULL DEFAULT 'none' CHECK (discount_type IN ('none','percent','amount')),
  discount_value numeric(14,2) NOT NULL DEFAULT 0 CHECK (discount_value >= 0),
  tax_rate_id    uuid,
  tax_name       text,
  tax_rate       numeric(6,3),
  line_total     numeric(14,2),
  is_addon       boolean NOT NULL DEFAULT false,
  FOREIGN KEY (tenant_id, estimate_id) REFERENCES estimates (tenant_id, id) ON DELETE CASCADE,
  FOREIGN KEY (tenant_id, tax_rate_id) REFERENCES tax_rates (tenant_id, id),
  CHECK (discount_type <> 'percent' OR discount_value <= 100)
);
CREATE INDEX estimate_items_parent ON estimate_items (estimate_id, sort_order);

-- ============================================================
-- Invoices
-- ============================================================

CREATE TABLE invoices (
  id                   uuid PRIMARY KEY,
  tenant_id            uuid NOT NULL REFERENCES tenants (id),
  customer_id          uuid NOT NULL,
  estimate_id          uuid,
  template_version_id  uuid NOT NULL,
  doc_type             text NOT NULL DEFAULT 'invoice' CHECK (doc_type = 'invoice'),
  bank_account_id      uuid,
  number               text,
  revision             int NOT NULL DEFAULT 0 CHECK (revision >= 0),
  previous_revision_id uuid UNIQUE,
  status               text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','final','superseded')),
  title                text,
  currency             char(3) NOT NULL,
  issue_date           date,
  due_date             date,
  terms                text,
  notes                text,
  subtotal             numeric(14,2),
  discount_total       numeric(14,2),
  tax_total            numeric(14,2),
  total                numeric(14,2),
  customer_snapshot    jsonb,
  issuer_snapshot      jsonb,
  pdf_path             text,
  pdf_sha256           text,
  finalised_at         timestamptz,
  finalised_by         uuid,
  created_at           timestamptz NOT NULL DEFAULT now(),
  updated_at           timestamptz NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, id),
  UNIQUE (tenant_id, number, revision),
  FOREIGN KEY (tenant_id, customer_id) REFERENCES customers (tenant_id, id),
  -- An invoice's customer must be its estimate's customer.
  FOREIGN KEY (tenant_id, estimate_id, customer_id) REFERENCES estimates (tenant_id, id, customer_id),
  FOREIGN KEY (tenant_id, template_version_id, doc_type) REFERENCES template_versions (tenant_id, id, doc_type),
  FOREIGN KEY (tenant_id, bank_account_id) REFERENCES bank_accounts (tenant_id, id),
  FOREIGN KEY (tenant_id, previous_revision_id) REFERENCES invoices (tenant_id, id),
  FOREIGN KEY (tenant_id, finalised_by) REFERENCES users (tenant_id, id),
  CHECK (status = 'draft' OR number IS NOT NULL),
  CHECK (status = 'draft' OR (total IS NOT NULL AND pdf_path IS NOT NULL AND finalised_at IS NOT NULL
                              AND customer_snapshot IS NOT NULL AND issuer_snapshot IS NOT NULL)),
  CHECK (due_date IS NULL OR issue_date IS NULL OR due_date >= issue_date)
);
CREATE UNIQUE INDEX invoices_one_final_per_number ON invoices (tenant_id, number) WHERE status = 'final';
CREATE INDEX invoices_customer ON invoices (tenant_id, customer_id);
CREATE INDEX invoices_estimate ON invoices (tenant_id, estimate_id);
CREATE INDEX invoices_created ON invoices (tenant_id, created_at DESC);

CREATE TABLE invoice_items (
  id             uuid PRIMARY KEY,
  tenant_id      uuid NOT NULL,
  invoice_id     uuid NOT NULL,
  sort_order     int NOT NULL,
  description    text NOT NULL CHECK (description <> ''),
  quantity       numeric(12,3) NOT NULL CHECK (quantity > 0),
  unit_price     numeric(14,2) NOT NULL CHECK (unit_price >= 0),
  discount_type  text NOT NULL DEFAULT 'none' CHECK (discount_type IN ('none','percent','amount')),
  discount_value numeric(14,2) NOT NULL DEFAULT 0 CHECK (discount_value >= 0),
  tax_rate_id    uuid,
  tax_name       text,
  tax_rate       numeric(6,3),
  line_total     numeric(14,2),
  FOREIGN KEY (tenant_id, invoice_id) REFERENCES invoices (tenant_id, id) ON DELETE CASCADE,
  FOREIGN KEY (tenant_id, tax_rate_id) REFERENCES tax_rates (tenant_id, id),
  CHECK (discount_type <> 'percent' OR discount_value <= 100)
);
CREATE INDEX invoice_items_parent ON invoice_items (invoice_id, sort_order);

-- ============================================================
-- Milestones
-- ============================================================

CREATE TABLE milestones (
  id          uuid PRIMARY KEY,
  tenant_id   uuid NOT NULL,
  estimate_id uuid NOT NULL,
  sort_order  int NOT NULL,
  name        text NOT NULL CHECK (name <> ''),
  note        text,
  percent     numeric(6,3) NOT NULL CHECK (percent > 0 AND percent <= 100),
  due_date    date,
  invoice_id  uuid UNIQUE,
  FOREIGN KEY (tenant_id, estimate_id) REFERENCES estimates (tenant_id, id) ON DELETE CASCADE,
  -- Deleting a draft milestone invoice un-bills the milestone (only invoice_id is nulled).
  FOREIGN KEY (tenant_id, invoice_id) REFERENCES invoices (tenant_id, id) ON DELETE SET NULL (invoice_id)
);
CREATE INDEX milestones_parent ON milestones (estimate_id, sort_order);

-- ============================================================
-- Audit
-- ============================================================

CREATE TABLE audit_logs (
  id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id       uuid NOT NULL REFERENCES tenants (id),
  user_id         uuid,
  via             text NOT NULL CHECK (via IN ('web','ai','system')),
  oauth_client_id uuid REFERENCES oauth_clients (id) ON DELETE SET NULL,
  request_id      text,
  action          text NOT NULL,
  entity          text NOT NULL,
  entity_id       text,
  changes         jsonb,
  created_at      timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (tenant_id, user_id) REFERENCES users (tenant_id, id)
);
CREATE INDEX audit_logs_entity ON audit_logs (tenant_id, entity, entity_id);
CREATE INDEX audit_logs_recent ON audit_logs (tenant_id, created_at DESC);

-- ============================================================
-- updated_at triggers
-- ============================================================

CREATE TRIGGER z_updated_at BEFORE UPDATE ON tenants          FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER z_updated_at BEFORE UPDATE ON users            FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER z_updated_at BEFORE UPDATE ON tax_rates        FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER z_updated_at BEFORE UPDATE ON bank_accounts    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER z_updated_at BEFORE UPDATE ON number_sequences FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER z_updated_at BEFORE UPDATE ON customers        FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER z_updated_at BEFORE UPDATE ON templates        FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER z_updated_at BEFORE UPDATE ON estimates        FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER z_updated_at BEFORE UPDATE ON invoices         FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ============================================================
-- Immutability of issued documents (SQLSTATE CQ001 = document locked)
-- ============================================================

-- +goose StatementBegin
CREATE FUNCTION guard_document() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    IF OLD.status <> 'draft' THEN
      RAISE EXCEPTION '% % is % and cannot be deleted', TG_TABLE_NAME, OLD.id, OLD.status USING ERRCODE = 'CQ001';
    END IF;
    RETURN OLD;
  END IF;

  IF OLD.status = 'draft' THEN
    RETURN NEW;
  END IF;

  -- The only change allowed on an issued document: final -> superseded.
  IF OLD.status = 'final' AND NEW.status = 'superseded'
     AND (to_jsonb(NEW) - 'status' - 'updated_at') = (to_jsonb(OLD) - 'status' - 'updated_at') THEN
    RETURN NEW;
  END IF;

  RAISE EXCEPTION '% % is % and cannot be changed; create a revision', TG_TABLE_NAME, OLD.id, OLD.status USING ERRCODE = 'CQ001';
END
$$;
-- +goose StatementEnd

CREATE TRIGGER a_guard BEFORE UPDATE OR DELETE ON estimates FOR EACH ROW EXECUTE FUNCTION guard_document();
CREATE TRIGGER a_guard BEFORE UPDATE OR DELETE ON invoices  FOR EACH ROW EXECUTE FUNCTION guard_document();

-- Children (items, milestones) may only change while the parent is a draft.
-- TG_ARGV[0] = parent table, TG_ARGV[1] = FK column. Milestones may additionally
-- have invoice_id set/cleared on a final estimate (billing).
-- +goose StatementBegin
CREATE FUNCTION guard_document_child() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
  parent_id     uuid;
  parent_status text;
BEGIN
  IF TG_OP = 'DELETE' THEN
    parent_id := (to_jsonb(OLD) ->> TG_ARGV[1])::uuid;
  ELSE
    parent_id := (to_jsonb(NEW) ->> TG_ARGV[1])::uuid;
  END IF;

  EXECUTE format('SELECT status FROM %I WHERE id = $1', TG_ARGV[0]) INTO parent_status USING parent_id;

  -- NULL: parent is being deleted (cascade from a draft delete).
  IF parent_status IS NULL OR parent_status = 'draft' THEN
    IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
  END IF;

  IF TG_OP = 'UPDATE' AND TG_TABLE_NAME = 'milestones'
     AND (to_jsonb(NEW) - 'invoice_id') = (to_jsonb(OLD) - 'invoice_id') THEN
    RETURN NEW;
  END IF;

  RAISE EXCEPTION '% belongs to a % document and cannot be changed', TG_TABLE_NAME, parent_status USING ERRCODE = 'CQ001';
END
$$;
-- +goose StatementEnd

CREATE TRIGGER a_guard BEFORE INSERT OR UPDATE OR DELETE ON estimate_items
  FOR EACH ROW EXECUTE FUNCTION guard_document_child('estimates', 'estimate_id');
CREATE TRIGGER a_guard BEFORE INSERT OR UPDATE OR DELETE ON invoice_items
  FOR EACH ROW EXECUTE FUNCTION guard_document_child('invoices', 'invoice_id');
CREATE TRIGGER a_guard BEFORE INSERT OR UPDATE OR DELETE ON milestones
  FOR EACH ROW EXECUTE FUNCTION guard_document_child('estimates', 'estimate_id');

-- Milestone percentages must total 100 when an estimate is finalised.
-- +goose StatementBegin
CREATE FUNCTION check_milestones_total() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
  n         int;
  total_pct numeric;
BEGIN
  SELECT count(*), coalesce(sum(percent), 0) INTO n, total_pct FROM milestones WHERE estimate_id = NEW.id;
  IF n > 0 AND total_pct <> 100 THEN
    RAISE EXCEPTION 'milestone percentages total %, must be 100', total_pct USING ERRCODE = 'check_violation';
  END IF;
  RETURN NULL;
END
$$;
-- +goose StatementEnd

CREATE TRIGGER milestones_total AFTER UPDATE OF status ON estimates
  FOR EACH ROW WHEN (OLD.status = 'draft' AND NEW.status = 'final')
  EXECUTE FUNCTION check_milestones_total();

-- ============================================================
-- Row-level security
-- ============================================================

ALTER TABLE tenants ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenants FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON tenants
  USING (id = app_current_tenant()) WITH CHECK (id = app_current_tenant());

-- +goose StatementBegin
DO $$
DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY[
    'tax_rates','bank_accounts','number_sequences','customers','templates','template_versions',
    'estimates','estimate_items','invoices','invoice_items','milestones','audit_logs'
  ] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id = app_current_tenant()) WITH CHECK (tenant_id = app_current_tenant())', t);
  END LOOP;
END
$$;
-- +goose StatementEnd

-- Fonts: shared (tenant_id NULL) rows are readable by all and may be added by any
-- tenant (fetched Google fonts); private rows are visible/changeable only by the owner.
ALTER TABLE fonts ENABLE ROW LEVEL SECURITY;
ALTER TABLE fonts FORCE ROW LEVEL SECURITY;
CREATE POLICY fonts_select ON fonts FOR SELECT USING (tenant_id IS NULL OR tenant_id = app_current_tenant());
CREATE POLICY fonts_insert ON fonts FOR INSERT WITH CHECK (tenant_id IS NULL OR tenant_id = app_current_tenant());
CREATE POLICY fonts_update ON fonts FOR UPDATE USING (tenant_id = app_current_tenant()) WITH CHECK (tenant_id = app_current_tenant());
CREATE POLICY fonts_delete ON fonts FOR DELETE USING (tenant_id = app_current_tenant());

ALTER TABLE font_files ENABLE ROW LEVEL SECURITY;
ALTER TABLE font_files FORCE ROW LEVEL SECURITY;
CREATE POLICY font_files_select ON font_files FOR SELECT USING (tenant_id IS NULL OR tenant_id = app_current_tenant());
CREATE POLICY font_files_insert ON font_files FOR INSERT WITH CHECK (tenant_id IS NULL OR tenant_id = app_current_tenant());
CREATE POLICY font_files_delete ON font_files FOR DELETE USING (tenant_id = app_current_tenant());

-- ============================================================
-- Grants beyond the default DML grant
-- ============================================================

-- Append-only tables.
REVOKE UPDATE, DELETE ON audit_logs FROM claviq_app;
REVOKE UPDATE, DELETE ON template_versions FROM claviq_app;
-- Tenants are created by the owner role (CLI), never by the app.
REVOKE INSERT, DELETE ON tenants FROM claviq_app;

-- +goose Down
DROP TABLE IF EXISTS audit_logs, milestones, invoice_items, invoices, estimate_items, estimates,
  font_files, fonts, template_versions, templates CASCADE;
DROP TABLE IF EXISTS customers, oauth_tokens, oauth_clients, number_sequences, bank_accounts,
  tax_rates, users, tenants CASCADE;
DROP FUNCTION IF EXISTS check_milestones_total();
DROP FUNCTION IF EXISTS guard_document_child();
DROP FUNCTION IF EXISTS guard_document();
DROP FUNCTION IF EXISTS font_files_copy_tenant();
