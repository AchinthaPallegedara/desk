// Package assets embeds the bundled default font (Poppins, OFL) and the
// default Typst templates seeded into every new tenant.
package assets

import "embed"

//go:embed fonts/poppins/*.ttf fonts/poppins/OFL.txt
var Fonts embed.FS

//go:embed templates/estimate.typ
var EstimateTemplate string

//go:embed templates/invoice.typ
var InvoiceTemplate string

// DefaultTemplate returns the built-in template source for a document type.
func DefaultTemplate(docType string) string {
	if docType == "invoice" {
		return InvoiceTemplate
	}
	return EstimateTemplate
}
