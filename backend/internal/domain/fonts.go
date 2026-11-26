package domain

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/claviq/desk/backend/assets"
	"github.com/claviq/desk/backend/internal/models"
	"github.com/claviq/desk/backend/internal/storage"
)

// FontLibrary keeps a local copy of font files for Typst. Storage (R2 in prod)
// is the durable copy; <data>/fonts mirrors it:
//
//	fonts/shared/<family>/...          shared library (bundled + Google)
//	fonts/tenants/<tenant>/<family>/... private uploads
//
// font_files.file_path holds the storage key, which equals the path under <data>/.
type FontLibrary struct {
	root  string // <data>
	store storage.Storage
	mu    sync.Mutex
	http  *http.Client
}

func NewFontLibrary(dataDir string, store storage.Storage) *FontLibrary {
	// Typst runs with its working directory set to a temp dir, so font paths must be absolute.
	if abs, err := filepath.Abs(dataDir); err == nil {
		dataDir = abs
	}
	return &FontLibrary{root: dataDir, store: store, http: &http.Client{Timeout: 30 * time.Second}}
}

func (f *FontLibrary) SharedDir() string { return filepath.Join(f.root, "fonts", "shared") }
func (f *FontLibrary) TenantDir(t uuid.UUID) string {
	return filepath.Join(f.root, "fonts", "tenants", t.String())
}
func (f *FontLibrary) local(key string) string { return filepath.Join(f.root, filepath.FromSlash(key)) }

var bundledWeights = map[string][2]any{
	"Light": {300, "normal"}, "Regular": {400, "normal"}, "Italic": {400, "italic"},
	"Medium": {500, "normal"}, "SemiBold": {600, "normal"}, "Bold": {700, "normal"},
}

// SyncBundled extracts the embedded Poppins files and registers them as a
// shared font. Idempotent; run at startup.
func (s *Service) SyncBundledFonts(ctx context.Context) error {
	f := s.fonts
	entries, err := fs.ReadDir(assets.Fonts, "fonts/poppins")
	if err != nil {
		return err
	}
	type file struct {
		key    string
		weight int
		style  string
	}
	var files []file
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".ttf") {
			continue
		}
		b, err := fs.ReadFile(assets.Fonts, "fonts/poppins/"+name)
		if err != nil {
			return err
		}
		key := "fonts/shared/poppins/" + name
		p := f.local(key)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			return err
		}
		if err := os.WriteFile(p, b, 0o640); err != nil {
			return err
		}
		variant := strings.TrimSuffix(strings.TrimPrefix(name, "Poppins-"), ".ttf")
		if w, ok := bundledWeights[variant]; ok {
			files = append(files, file{key, w[0].(int), w[1].(string)})
		}
	}

	// Shared rows: tenant_id NULL is permitted by RLS without a tenant context.
	return mapError(s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var font models.Font
		err := tx.Where("tenant_id IS NULL AND lower(family) = 'poppins'").First(&font).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			lic := "SIL Open Font License 1.1"
			font = models.Font{Family: "Poppins", Source: "bundled", Licence: &lic}
			if err := tx.Create(&font).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		for _, fl := range files {
			ff := models.FontFile{FontID: font.ID, Weight: fl.weight, Style: fl.style, FilePath: fl.key}
			if err := tx.Where("font_id = ? AND weight = ? AND style = ?", font.ID, fl.weight, fl.style).
				FirstOrCreate(&ff).Error; err != nil {
				return err
			}
		}
		return nil
	}))
}

// fontDirs makes sure every font file visible to the tenant exists locally and
// returns the directories Typst may read: shared library + the tenant's own.
func (s *Service) fontDirs(ctx context.Context, tx *gorm.DB, tenantID uuid.UUID) ([]string, error) {
	var files []models.FontFile
	if err := tx.Find(&files).Error; err != nil { // RLS: shared + this tenant's
		return nil, err
	}
	f := s.fonts
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, ff := range files {
		p := f.local(ff.FilePath)
		if _, err := os.Stat(p); err == nil {
			continue
		}
		b, err := s.store.Get(ctx, ff.FilePath)
		if err != nil {
			s.log.Warn("font file missing", "key", ff.FilePath, "error", err)
			continue // templates declare fallbacks; a missing font must not break a PDF
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			return nil, err
		}
		if err := os.WriteFile(p, b, 0o640); err != nil {
			return nil, err
		}
	}
	dirs := []string{f.SharedDir()}
	if td := f.TenantDir(tenantID); dirExists(td) {
		dirs = append(dirs, td)
	}
	return dirs, nil
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// FontView lists a family with its available weights for the AI/web.
type FontView struct {
	ID       uuid.UUID `json:"id"`
	Family   string    `json:"family"`
	Scope    string    `json:"scope"` // shared | private
	Source   string    `json:"source"`
	Licence  *string   `json:"licence,omitempty"`
	Variants []string  `json:"variants"` // e.g. "400", "700 italic"
}

func (s *Service) ListFonts(ctx context.Context, a Actor) ([]FontView, error) {
	var fonts []models.Font
	err := s.tx(ctx, a, func(tx *gorm.DB) error {
		return tx.Preload("Files", func(db *gorm.DB) *gorm.DB { return db.Order("weight, style") }).
			Order("lower(family)").Find(&fonts).Error
	})
	if err != nil {
		return nil, err
	}
	out := make([]FontView, 0, len(fonts))
	for _, f := range fonts {
		v := FontView{ID: f.ID, Family: f.Family, Scope: "shared", Source: f.Source, Licence: f.Licence}
		if f.TenantID != nil {
			v.Scope = "private"
		}
		for _, ff := range f.Files {
			lbl := strconv.Itoa(ff.Weight)
			if ff.Style == "italic" {
				lbl += " italic"
			}
			v.Variants = append(v.Variants, lbl)
		}
		out = append(out, v)
	}
	return out, nil
}

var (
	fontFaceRe = regexp.MustCompile(`(?s)@font-face\s*\{(.*?)\}`)
	fontStyle  = regexp.MustCompile(`font-style:\s*(normal|italic)`)
	fontWeight = regexp.MustCompile(`font-weight:\s*(\d+)`)
	fontSrc    = regexp.MustCompile(`url\((https://fonts\.gstatic\.com/[^)]+)\)\s*format\('(truetype|opentype)'\)`)
	familyRe   = regexp.MustCompile(`^[A-Za-z0-9 ]{2,64}$`)
)

// AddGoogleFont fetches an open-licence Google Font into the shared library.
// Once fetched for one tenant it is available to all.
func (s *Service) AddGoogleFont(ctx context.Context, a Actor, family string) (*FontView, error) {
	family = strings.Join(strings.Fields(family), " ")
	if !familyRe.MatchString(family) {
		return nil, invalid("font family must be 2–64 letters, digits or spaces")
	}
	if existing, err := s.findFont(ctx, a, family); err != nil || existing != nil {
		return existing, err
	}

	css, err := s.fetchGoogleCSS(ctx, family)
	if err != nil {
		return nil, err
	}
	type face struct {
		weight int
		style  string
		url    string
	}
	seen := map[string]bool{}
	var faces []face
	for _, m := range fontFaceRe.FindAllStringSubmatch(css, -1) {
		body := m[1]
		st, wt, src := fontStyle.FindStringSubmatch(body), fontWeight.FindStringSubmatch(body), fontSrc.FindStringSubmatch(body)
		if st == nil || wt == nil || src == nil {
			continue
		}
		w, _ := strconv.Atoi(wt[1])
		k := wt[1] + st[1]
		if seen[k] {
			continue
		}
		seen[k] = true
		faces = append(faces, face{w, st[1], src[1]})
	}
	if len(faces) == 0 {
		return nil, invalid("Google Fonts returned no downloadable files for %q", family)
	}

	slug := strings.ToLower(strings.ReplaceAll(family, " ", "-"))
	type stored struct {
		key    string
		weight int
		style  string
	}
	var files []stored
	for _, fc := range faces {
		b, err := s.download(ctx, fc.url, 8<<20)
		if err != nil {
			return nil, fmt.Errorf("download %s %d: %w", family, fc.weight, err)
		}
		if !isFontFile(b) {
			return nil, fmt.Errorf("download %s: not a font file", family)
		}
		key := fmt.Sprintf("fonts/shared/%s/%s-%d-%s.ttf", slug, slug, fc.weight, fc.style)
		if err := s.store.Put(ctx, key, b, "font/ttf"); err != nil {
			return nil, err
		}
		p := s.fonts.local(key)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err == nil {
			_ = os.WriteFile(p, b, 0o640)
		}
		files = append(files, stored{key, fc.weight, fc.style})
	}

	var font models.Font
	err = s.tx(ctx, a, func(tx *gorm.DB) error {
		lic := "Open licence (Google Fonts: OFL / Apache 2.0)"
		font = models.Font{Family: family, Source: "google", Licence: &lic}
		if err := tx.Create(&font).Error; err != nil {
			return err
		}
		for _, f := range files {
			if err := tx.Create(&models.FontFile{FontID: font.ID, Weight: f.weight, Style: f.style, FilePath: f.key}).Error; err != nil {
				return err
			}
		}
		return s.audit(tx, a, "add_google_font", "font", font.ID, map[string]any{"family": family, "files": len(files)})
	})
	if err != nil {
		return nil, err
	}
	return s.findFont(ctx, a, family)
}

func (s *Service) findFont(ctx context.Context, a Actor, family string) (*FontView, error) {
	all, err := s.ListFonts(ctx, a)
	if err != nil {
		return nil, err
	}
	for _, f := range all {
		if strings.EqualFold(f.Family, family) {
			return &f, nil
		}
	}
	return nil, nil
}

func (s *Service) fetchGoogleCSS(ctx context.Context, family string) (string, error) {
	fam := url.QueryEscape(family)
	attempts := []string{
		fam + ":ital,wght@0,300;0,400;0,500;0,600;0,700;1,400;1,700",
		fam + ":wght@400;500;600;700",
		fam + ":wght@400;700",
		fam,
	}
	for _, q := range attempts {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://fonts.googleapis.com/css2?family="+q, nil)
		// A non-browser User-Agent makes Google serve TrueType instead of WOFF2.
		req.Header.Set("User-Agent", "claviq-desk/1.0")
		resp, err := s.fonts.http.Do(req)
		if err != nil {
			return "", fmt.Errorf("google fonts: %w", err)
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return string(body), nil
		}
	}
	return "", invalid("%q was not found on Google Fonts", family)
}

func (s *Service) download(ctx context.Context, u string, max int64) ([]byte, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	resp, err := s.fonts.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, errors.New("file too large")
	}
	return b, nil
}

func isFontFile(b []byte) bool {
	if len(b) < 4 {
		return false
	}
	m := string(b[:4])
	return m == "\x00\x01\x00\x00" || m == "true" || m == "OTTO"
}

// UploadFont adds a private font file for this tenant only (paid licences are never shared).
// family must match the font's internal family name, which is what templates reference.
func (s *Service) UploadFont(ctx context.Context, a Actor, family string, weight int, style string, data []byte, licence string) (*FontView, error) {
	family = strings.Join(strings.Fields(family), " ")
	if !familyRe.MatchString(family) {
		return nil, invalid("font family must be 2–64 letters, digits or spaces")
	}
	if weight < 100 || weight > 900 || weight%100 != 0 {
		return nil, invalid("weight must be 100–900 in steps of 100")
	}
	if style != "normal" && style != "italic" {
		return nil, invalid("style must be normal or italic")
	}
	if len(data) > 15<<20 || !isFontFile(data) {
		return nil, invalid("upload a TrueType or OpenType font file (max 15 MB)")
	}
	slug := strings.ToLower(strings.ReplaceAll(family, " ", "-"))
	ext := "ttf"
	if string(data[:4]) == "OTTO" {
		ext = "otf"
	}
	key := fmt.Sprintf("fonts/tenants/%s/%s/%s-%d-%s.%s", a.TenantID, slug, slug, weight, style, ext)
	if err := s.store.Put(ctx, key, data, "font/"+ext); err != nil {
		return nil, err
	}
	p := s.fonts.local(key)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err == nil {
		_ = os.WriteFile(p, data, 0o640)
	}

	err := s.tx(ctx, a, func(tx *gorm.DB) error {
		var font models.Font
		err := tx.Where("tenant_id = ? AND lower(family) = lower(?)", a.TenantID, family).First(&font).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			tid := a.TenantID
			font = models.Font{Family: family, TenantID: &tid, Source: "upload", Licence: trimPtr(&licence)}
			if err := tx.Create(&font).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if err := tx.Where("font_id = ? AND weight = ? AND style = ?", font.ID, weight, style).
			Delete(&models.FontFile{}).Error; err != nil {
			return err
		}
		if err := tx.Create(&models.FontFile{FontID: font.ID, Weight: weight, Style: style, FilePath: key}).Error; err != nil {
			return err
		}
		return s.audit(tx, a, "upload_font", "font", font.ID, map[string]any{"family": family, "weight": weight, "style": style})
	})
	if err != nil {
		return nil, err
	}
	return s.findFont(ctx, a, family)
}
