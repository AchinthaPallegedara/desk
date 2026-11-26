package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/claviq/desk/backend/internal/domain"
)

func (s *Server) registerTemplateTools(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{Name: "get_template_guide", Annotations: readOnly("Template guide"),
		Description: "How Claviq templates work: the data.json fields available to Typst, sandbox rules and design conventions. Read before writing a template."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ empty) (*mcp.CallToolResult, any, error) {
			if _, err := s.actor(ctx); err != nil {
				return nil, nil, err
			}
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: domain.TemplateGuide}}}, nil, nil
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "list_templates", Annotations: readOnly("List templates"),
		Description: "List estimate and invoice templates (id, name, default flag, current version)."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in listTemplatesInput) (*mcp.CallToolResult, any, error) {
			a, err := s.actor(ctx)
			if err != nil {
				return nil, nil, err
			}
			ts, err := s.svc.ListTemplates(ctx, a, trimLower(in.DocType), false)
			if err != nil {
				return nil, nil, s.toolErr(err)
			}
			return nil, map[string]any{"templates": ts}, nil
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "get_template", Annotations: readOnly("Get template"),
		Description: "Get a template including its current Typst source (edit it, then save with update_template)."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, any, error) {
			a, err := s.actor(ctx)
			if err != nil {
				return nil, nil, err
			}
			id, err := parseID(in.ID, "id")
			if err != nil {
				return nil, nil, err
			}
			t, err := s.svc.GetTemplate(ctx, a, id)
			if err != nil {
				return nil, nil, s.toolErr(err)
			}
			return nil, t, nil
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "create_template", Annotations: writes("Create template"),
		Description: "Create an estimate or invoice template. The source must compile against sample data; compile errors are returned so you can fix them. Returns a preview link with sample data."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in templateCreateInput) (*mcp.CallToolResult, any, error) {
			a, err := s.actor(ctx)
			if err != nil {
				return nil, nil, err
			}
			ti := domain.TemplateInput{Name: &in.Name, DocType: trimLower(in.DocType), IsDefault: &in.IsDefault}
			if in.TypstSource != "" {
				ti.Source = &in.TypstSource
			}
			t, err := s.svc.CreateTemplate(ctx, a, ti)
			if err != nil {
				return nil, nil, s.toolErr(err)
			}
			return s.templateResult(ctx, a, t)
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "update_template", Annotations: writes("Update template"),
		Description: "Rename a template, make it the default, and/or save new Typst source as a new version (old versions stay restorable). Draft documents pick up the new version automatically."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in templateUpdateInput) (*mcp.CallToolResult, any, error) {
			a, err := s.actor(ctx)
			if err != nil {
				return nil, nil, err
			}
			id, err := parseID(in.TemplateID, "template_id")
			if err != nil {
				return nil, nil, err
			}
			t, err := s.svc.UpdateTemplate(ctx, a, id, domain.TemplateInput{Name: in.Name, Source: in.TypstSource, IsDefault: in.IsDefault})
			if err != nil {
				return nil, nil, s.toolErr(err)
			}
			return s.templateResult(ctx, a, t)
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "list_template_versions", Annotations: readOnly("Template versions"),
		Description: "List saved versions of a template (newest first)."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, any, error) {
			a, err := s.actor(ctx)
			if err != nil {
				return nil, nil, err
			}
			id, err := parseID(in.ID, "id")
			if err != nil {
				return nil, nil, err
			}
			vs, err := s.svc.ListTemplateVersions(ctx, a, id)
			if err != nil {
				return nil, nil, s.toolErr(err)
			}
			return nil, map[string]any{"versions": vs}, nil
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "restore_template_version", Annotations: writes("Restore template version"),
		Description: "Make an older version current again (saved as a new version)."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in templateVersionInput) (*mcp.CallToolResult, any, error) {
			a, err := s.actor(ctx)
			if err != nil {
				return nil, nil, err
			}
			tid, err := parseID(in.TemplateID, "template_id")
			if err != nil {
				return nil, nil, err
			}
			vid, err := parseID(in.VersionID, "version_id")
			if err != nil {
				return nil, nil, err
			}
			t, err := s.svc.RestoreTemplateVersion(ctx, a, tid, vid)
			if err != nil {
				return nil, nil, s.toolErr(err)
			}
			return s.templateResult(ctx, a, t)
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "preview_template", Annotations: readOnly("Preview template"),
		Description: "Preview link for a template rendered with sample data."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, any, error) {
			a, err := s.actor(ctx)
			if err != nil {
				return nil, nil, err
			}
			id, err := parseID(in.ID, "id")
			if err != nil {
				return nil, nil, err
			}
			l, err := s.svc.TemplatePreviewLinks(ctx, a, id, nil)
			if err != nil {
				return nil, nil, s.toolErr(err)
			}
			return nil, l, nil
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "list_fonts", Annotations: readOnly("List fonts"),
		Description: "Fonts available to templates (shared library + this business's private uploads) with their weights. Check before choosing a font."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ empty) (*mcp.CallToolResult, any, error) {
			a, err := s.actor(ctx)
			if err != nil {
				return nil, nil, err
			}
			f, err := s.svc.ListFonts(ctx, a)
			if err != nil {
				return nil, nil, s.toolErr(err)
			}
			return nil, map[string]any{"fonts": f}, nil
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "add_google_font", Annotations: &mcp.ToolAnnotations{Title: "Add Google font", DestructiveHint: boolPtr(false), OpenWorldHint: boolPtr(true)},
		Description: "Fetch an open-licence font from Google Fonts into the shared library so templates can use it."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in fontInput) (*mcp.CallToolResult, any, error) {
			a, err := s.actor(ctx)
			if err != nil {
				return nil, nil, err
			}
			f, err := s.svc.AddGoogleFont(ctx, a, in.Family)
			if err != nil {
				return nil, nil, s.toolErr(err)
			}
			return nil, f, nil
		})
}

func (s *Server) templateResult(ctx context.Context, a domain.Actor, t *domain.TemplateView) (*mcp.CallToolResult, any, error) {
	l, err := s.svc.TemplatePreviewLinks(ctx, a, t.ID, nil)
	if err != nil {
		return nil, nil, s.toolErr(err)
	}
	t.Source = "" // don't echo large sources back
	return nil, map[string]any{"template": t, "preview_url": l.PreviewURL}, nil
}
