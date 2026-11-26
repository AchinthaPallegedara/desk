// Package render compiles Typst templates into PDF/PNG in a sandbox:
// a fresh temp dir as --root (template, data.json, assets only), explicit font
// dirs only, no system fonts, no package downloads, a hard time limit and a
// small concurrency cap for the shared VPS.
package render

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	MaxSourceBytes = 256 * 1024
	defaultTimeout = 15 * time.Second
	maxConcurrent  = 2
)

// CompileError is a template/data problem (shown to the user / AI), as opposed
// to an infrastructure failure.
type CompileError struct{ Message string }

func (e *CompileError) Error() string { return "typst: " + e.Message }

type Renderer struct {
	bin     string
	timeout time.Duration
	sem     chan struct{}
}

func New(bin string) *Renderer {
	return &Renderer{bin: bin, timeout: defaultTimeout, sem: make(chan struct{}, maxConcurrent)}
}

type Format string

const (
	PDF Format = "pdf"
	PNG Format = "png"
)

type Job struct {
	Source   string
	Data     any               // written to data.json
	Assets   map[string][]byte // extra files in the root, e.g. "logo.png"
	FontDirs []string
	Format   Format
	PPI      int // PNG only
}

type Result struct {
	PDF   []byte
	Pages [][]byte // PNG pages
}

var packageImport = regexp.MustCompile(`["']@[a-z0-9-]+/`)

// ValidateSource rejects things the sandbox doesn't allow before running Typst.
func ValidateSource(src string) error {
	if strings.TrimSpace(src) == "" {
		return &CompileError{Message: "template source is empty"}
	}
	if len(src) > MaxSourceBytes {
		return &CompileError{Message: fmt.Sprintf("template source exceeds %d KB", MaxSourceBytes/1024)}
	}
	if packageImport.MatchString(src) {
		return &CompileError{Message: "Typst packages (@preview/...) are not allowed; write the layout inline"}
	}
	return nil
}

func (r *Renderer) Render(ctx context.Context, job Job) (*Result, error) {
	if err := ValidateSource(job.Source); err != nil {
		return nil, err
	}

	select {
	case r.sem <- struct{}{}:
		defer func() { <-r.sem }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	dir, err := os.MkdirTemp("", "claviq-typst-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	root := filepath.Join(dir, "root")
	pkgDir := filepath.Join(dir, "pkg")
	outDir := filepath.Join(dir, "out")
	for _, d := range []string{root, pkgDir, outDir} {
		if err := os.Mkdir(d, 0o700); err != nil {
			return nil, err
		}
	}

	data, err := json.Marshal(job.Data)
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{"main.typ": []byte(job.Source), "data.json": data}
	for name, b := range job.Assets {
		if strings.ContainsAny(name, `/\`) || name == "main.typ" || name == "data.json" {
			return nil, fmt.Errorf("render: invalid asset name %q", name)
		}
		files[name] = b
	}
	for name, b := range files {
		if err := os.WriteFile(filepath.Join(root, name), b, 0o600); err != nil {
			return nil, err
		}
	}

	format := job.Format
	if format == "" {
		format = PDF
	}
	out := filepath.Join(outDir, "out.pdf")
	if format == PNG {
		out = filepath.Join(outDir, "page-{0p}.png")
	}

	args := []string{"compile",
		"--root", root,
		"--ignore-system-fonts",
		"--package-path", pkgDir,
		"--package-cache-path", pkgDir,
		"--diagnostic-format", "short",
		"--format", string(format),
	}
	for _, fd := range job.FontDirs {
		if fd != "" {
			args = append(args, "--font-path", fd)
		}
	}
	if format == PNG {
		ppi := job.PPI
		if ppi <= 0 {
			ppi = 144
		}
		args = append(args, "--ppi", fmt.Sprint(ppi))
	}
	args = append(args, filepath.Join(root, "main.typ"), out)

	cctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, r.bin, args...)
	cmd.Dir = dir
	// Minimal environment: no inherited secrets, caches confined to the temp dir.
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "XDG_CACHE_HOME=" + dir, "XDG_DATA_HOME=" + dir}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if errors.Is(cctx.Err(), context.DeadlineExceeded) {
			return nil, &CompileError{Message: fmt.Sprintf("rendering took longer than %s and was stopped", r.timeout)}
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			msg := strings.ReplaceAll(stderr.String(), root+string(filepath.Separator), "")
			msg = strings.ReplaceAll(msg, dir, "")
			msg = strings.ReplaceAll(msg, "root"+string(filepath.Separator), "") // path relative to cmd.Dir
			return nil, &CompileError{Message: strings.TrimSpace(msg)}
		}
		return nil, fmt.Errorf("render: run typst: %w", err)
	}

	res := &Result{}
	if format == PDF {
		res.PDF, err = os.ReadFile(out)
		return res, err
	}
	pages, _ := filepath.Glob(filepath.Join(outDir, "page-*.png"))
	sort.Strings(pages)
	for _, p := range pages {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		res.Pages = append(res.Pages, b)
	}
	return res, nil
}
