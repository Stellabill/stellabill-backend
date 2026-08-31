// Package adr validates Nygard-style Architecture Decision Records and
// generates the docs/adr index.
package adr

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	DefaultDir     = "docs/adr"
	TemplateName   = "0000-template.md"
	IndexName      = "README.md"
	AdrDirFileName = ".adr-dir"
)

var (
	fileNameRE = regexp.MustCompile(`^(\d{4})-([a-z0-9]+(?:-[a-z0-9]+)*)\.md$`)
	titleRE    = regexp.MustCompile(`^#\s+(\d{4})\.\s+(.+)$`)
	h2RE       = regexp.MustCompile(`^##\s+(.+)$`)
)

// AllowedStatuses are valid ADR Status section values (first non-empty line).
var AllowedStatuses = map[string]struct{}{
	"Proposed":   {},
	"Accepted":   {},
	"Deprecated": {},
	"Superseded": {},
	"Rejected":   {},
}

// requiredSections must appear as H2 headings in order (extra H2s allowed after).
var requiredSections = []string{"Status", "Context", "Decision", "Consequences"}

// Record is a parsed ADR markdown file.
type Record struct {
	Path       string
	Number     int
	Slug       string
	Title      string
	Status     string
	IsTemplate bool
}

// ReadAdrDir resolves the ADR directory from .adr-dir (if present) or DefaultDir.
func ReadAdrDir(repoRoot string) (string, error) {
	cfg := filepath.Join(repoRoot, AdrDirFileName)
	data, err := os.ReadFile(cfg)
	if err != nil {
		if os.IsNotExist(err) {
			return filepath.Join(repoRoot, DefaultDir), nil
		}
		return "", err
	}
	line := strings.TrimSpace(string(data))
	if line == "" {
		return filepath.Join(repoRoot, DefaultDir), nil
	}
	if filepath.IsAbs(line) {
		return line, nil
	}
	return filepath.Join(repoRoot, line), nil
}

// LoadDir loads ADR records from dir (non-recursive).
func LoadDir(dir string) ([]Record, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read adr dir: %w", err)
	}

	var out []Record
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".md") {
			continue
		}
		if ent.Name() == IndexName || ent.Name() == "ADR_TOOLS.md" || ent.Name() == "VERIFICATION.md" {
			continue
		}
		path := filepath.Join(dir, ent.Name())
		rec, err := ParseFile(path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", ent.Name(), err)
		}
		out = append(out, rec)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out, nil
}

// ParseFile parses a single ADR markdown file.
func ParseFile(path string) (Record, error) {
	base := filepath.Base(path)
	m := fileNameRE.FindStringSubmatch(base)
	if m == nil {
		return Record{}, fmt.Errorf("filename %q must match NNNN-kebab-case.md", base)
	}
	num, _ := strconv.Atoi(m[1])
	slug := m[2]

	raw, err := os.ReadFile(path)
	if err != nil {
		return Record{}, err
	}

	rec := Record{
		Path:       path,
		Number:     num,
		Slug:       slug,
		IsTemplate: num == 0 && base == TemplateName,
	}

	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var (
		foundTitle bool
		section    string
		statusBuf  []string
		h2s        []string
	)

	for scanner.Scan() {
		line := scanner.Text()
		if !foundTitle {
			tm := titleRE.FindStringSubmatch(strings.TrimSpace(line))
			if tm != nil {
				foundTitle = true
				titleNum, _ := strconv.Atoi(tm[1])
				if titleNum != num {
					return Record{}, fmt.Errorf("title number %04d does not match filename %04d", titleNum, num)
				}
				rec.Title = strings.TrimSpace(tm[2])
			}
			continue
		}

		if hm := h2RE.FindStringSubmatch(line); hm != nil {
			section = strings.TrimSpace(hm[1])
			h2s = append(h2s, section)
			continue
		}

		if section == "Status" {
			trim := strings.TrimSpace(line)
			if trim == "" || strings.HasPrefix(trim, "<!--") {
				continue
			}
			statusBuf = append(statusBuf, trim)
		}
	}
	if err := scanner.Err(); err != nil {
		return Record{}, err
	}

	if !foundTitle {
		return Record{}, fmt.Errorf("missing H1 title of form '# %04d. …'", num)
	}

	if err := validateSections(h2s); err != nil {
		return Record{}, err
	}

	if len(statusBuf) == 0 {
		return Record{}, fmt.Errorf("Status section is empty")
	}
	status := statusBuf[0]
	// Allow "Superseded by …" → treat first word
	statusWord := strings.Fields(status)[0]
	if _, ok := AllowedStatuses[statusWord]; !ok {
		return Record{}, fmt.Errorf("invalid Status %q (want one of Proposed|Accepted|Deprecated|Superseded|Rejected)", status)
	}
	rec.Status = statusWord

	return rec, nil
}

func validateSections(h2s []string) error {
	idx := 0
	for _, want := range requiredSections {
		for idx < len(h2s) && h2s[idx] != want {
			idx++
		}
		if idx >= len(h2s) {
			return fmt.Errorf("missing required section ## %s", want)
		}
		idx++
	}
	return nil
}

// ValidateUniqueNumbers ensures ADR numbers are unique (template 0000 allowed once).
func ValidateUniqueNumbers(recs []Record) error {
	seen := map[int]string{}
	for _, r := range recs {
		if prev, ok := seen[r.Number]; ok {
			return fmt.Errorf("duplicate ADR number %04d: %s and %s", r.Number, prev, filepath.Base(r.Path))
		}
		seen[r.Number] = filepath.Base(r.Path)
	}
	return nil
}

// ValidateTemplatePresent ensures 0000-template.md exists.
func ValidateTemplatePresent(recs []Record) error {
	for _, r := range recs {
		if r.IsTemplate {
			return nil
		}
	}
	return fmt.Errorf("missing required template %s", TemplateName)
}

// GenerateIndex renders README.md content for the ADR directory.
func GenerateIndex(recs []Record) string {
	var b strings.Builder
	b.WriteString("# Architecture Decision Records\n\n")
	b.WriteString("Nygard-style ADRs for Stellabill backend.\n\n")
	b.WriteString("> This index is **auto-generated** by `make adr-index` / `go run ./cmd/adr-lint -write-index`.\n")
	b.WriteString("> Do not edit by hand — update individual ADR files instead.\n\n")
	b.WriteString("See [ADR_TOOLS.md](ADR_TOOLS.md) for authoring rules and [0000-template.md](0000-template.md) for the template.\n\n")
	b.WriteString("| ADR | Title | Status |\n")
	b.WriteString("| --- | --- | --- |\n")

	for _, r := range recs {
		if r.IsTemplate {
			continue
		}
		name := filepath.Base(r.Path)
		fmt.Fprintf(&b, "| [%04d](%s) | %s | %s |\n", r.Number, name, r.Title, r.Status)
	}

	b.WriteString("\n## Template\n\n")
	b.WriteString("| File | Purpose |\n")
	b.WriteString("| --- | --- |\n")
	b.WriteString("| [0000-template.md](0000-template.md) | Copy this file when adding a new ADR |\n")
	return b.String()
}

// Lint loads, validates uniqueness/template/structure, and optionally checks index freshness.
func Lint(repoRoot string, checkIndex bool) ([]Record, error) {
	dir, err := ReadAdrDir(repoRoot)
	if err != nil {
		return nil, err
	}
	recs, err := LoadDir(dir)
	if err != nil {
		return nil, err
	}
	if err := ValidateTemplatePresent(recs); err != nil {
		return nil, err
	}
	if err := ValidateUniqueNumbers(recs); err != nil {
		return nil, err
	}
	if checkIndex {
		want := GenerateIndex(recs)
		got, err := os.ReadFile(filepath.Join(dir, IndexName))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w (run make adr-index)", IndexName, err)
		}
		if normalize(string(got)) != normalize(want) {
			return nil, fmt.Errorf("%s is stale; run `make adr-index` and commit the result", IndexName)
		}
	}
	return recs, nil
}

func normalize(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.TrimSpace(s) + "\n"
}

// WriteIndex writes the generated index to docs/adr/README.md.
func WriteIndex(repoRoot string) error {
	dir, err := ReadAdrDir(repoRoot)
	if err != nil {
		return err
	}
	recs, err := LoadDir(dir)
	if err != nil {
		return err
	}
	if err := ValidateUniqueNumbers(recs); err != nil {
		return err
	}
	content := GenerateIndex(recs)
	return os.WriteFile(filepath.Join(dir, IndexName), []byte(content), 0o644)
}
