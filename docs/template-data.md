# Template data (Typst)

The canonical text lives in `backend/internal/domain/template_guide.go` (served to AI clients by the
`get_template_guide` MCP tool) and must match `RenderData` in `backend/internal/domain/rendering.go`.
Default templates: `backend/assets/templates/{estimate,invoice}.typ`.

Key points:
- Templates read `json("data.json")`; money is pre-formatted text, empty optional values are `""`, `bank` may be `none`.
- Sandbox: only `main.typ`, `data.json` and the logo file are readable; `@preview` packages are rejected; 15 s limit;
  fonts come only from the shared library and the tenant's private folder (no system fonts).
- Every saved version is compiled against `SampleData()` first, so a broken template can never become current.
- Drafts always render with the template's current version; final documents keep the version they were issued with.
