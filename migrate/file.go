package migrate

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	revisionPrefix     = "-- revision: "
	downRevisionPrefix = "-- down_revision: "
	upMarker           = "-- migrate:up"
	downMarker         = "-- migrate:down"
)

// Migration is one migration file: a revision, its parent (down_revision,
// noneRevision if it has none), and the SQL that applies (UpSQL) or
// undoes (DownSQL) it.
type Migration struct {
	Revision     string
	DownRevision string
	UpSQL        string
	DownSQL      string
}

// Parse reads a migration file's contents. It is a small line scanner,
// not a regular expression engine: it requires the two header lines in
// order, then a "-- migrate:up" marker followed later by a
// "-- migrate:down" marker, each followed by that section's SQL.
// Malformed input (missing header, missing or out-of-order markers,
// empty sections) is a parse error, never a panic.
func Parse(data []byte) (Migration, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))

	var m Migration
	if !scanner.Scan() {
		return Migration{}, fmt.Errorf("migrate: empty migration file")
	}
	line := scanner.Text()
	if !strings.HasPrefix(line, revisionPrefix) {
		return Migration{}, fmt.Errorf("migrate: expected %q as the first line, got %q", strings.TrimSpace(revisionPrefix), line)
	}
	m.Revision = strings.TrimSpace(strings.TrimPrefix(line, revisionPrefix))

	if !scanner.Scan() {
		return Migration{}, fmt.Errorf("migrate: missing %q line", strings.TrimSpace(downRevisionPrefix))
	}
	line = scanner.Text()
	if !strings.HasPrefix(line, downRevisionPrefix) {
		return Migration{}, fmt.Errorf("migrate: expected %q as the second line, got %q", strings.TrimSpace(downRevisionPrefix), line)
	}
	m.DownRevision = strings.TrimSpace(strings.TrimPrefix(line, downRevisionPrefix))

	var upLines, downLines []string
	section, sawUp, sawDown := "", false, false
	for scanner.Scan() {
		line := scanner.Text()
		switch strings.TrimSpace(line) {
		case upMarker:
			if sawUp {
				return Migration{}, fmt.Errorf("migrate: duplicate %q marker", upMarker)
			}
			if sawDown {
				return Migration{}, fmt.Errorf("migrate: %q marker must come before %q", upMarker, downMarker)
			}
			sawUp, section = true, "up"
			continue
		case downMarker:
			if !sawUp {
				return Migration{}, fmt.Errorf("migrate: %q marker found before %q", downMarker, upMarker)
			}
			if sawDown {
				return Migration{}, fmt.Errorf("migrate: duplicate %q marker", downMarker)
			}
			sawDown, section = true, "down"
			continue
		}
		switch section {
		case "up":
			upLines = append(upLines, line)
		case "down":
			downLines = append(downLines, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return Migration{}, fmt.Errorf("migrate: reading migration file: %w", err)
	}
	if !sawUp {
		return Migration{}, fmt.Errorf("migrate: missing %q marker", upMarker)
	}
	if !sawDown {
		return Migration{}, fmt.Errorf("migrate: missing %q marker", downMarker)
	}

	m.UpSQL = strings.TrimSpace(strings.Join(upLines, "\n"))
	m.DownSQL = strings.TrimSpace(strings.Join(downLines, "\n"))
	if m.UpSQL == "" {
		return Migration{}, fmt.Errorf("migrate: empty %q section", upMarker)
	}
	if m.DownSQL == "" {
		return Migration{}, fmt.Errorf("migrate: empty %q section", downMarker)
	}
	return m, nil
}

// Write renders m to the standard file format and saves it under dir,
// named "<revision>_<slug>.sql". It creates dir if needed and returns the
// path written.
func Write(dir string, m Migration, slug string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("migrate: creating migrations directory: %w", err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s%s\n", revisionPrefix, m.Revision)
	fmt.Fprintf(&b, "%s%s\n", downRevisionPrefix, m.DownRevision)
	b.WriteString(upMarker + "\n")
	b.WriteString(m.UpSQL)
	b.WriteString("\n\n" + downMarker + "\n")
	b.WriteString(m.DownSQL)
	b.WriteString("\n")

	path := filepath.Join(dir, fmt.Sprintf("%s_%s.sql", m.Revision, slugify(slug)))
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return "", fmt.Errorf("migrate: writing migration file: %w", err)
	}
	return path, nil
}

// LoadAll reads and parses every *.sql file in dir, sorted by filename. A
// missing directory is treated as zero migrations, not an error, since a
// project that has never generated one yet has no reason to have created
// it.
func LoadAll(dir string) ([]Migration, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("migrate: reading migrations directory: %w", err)
	}

	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	migrations := make([]Migration, 0, len(names))
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("migrate: reading %s: %w", name, err)
		}
		m, err := Parse(data)
		if err != nil {
			return nil, fmt.Errorf("migrate: parsing %s: %w", name, err)
		}
		migrations = append(migrations, m)
	}
	return migrations, nil
}

// slugify turns an arbitrary message into a short, filename-safe slug:
// lowercased, non-alphanumeric runs collapsed to a single underscore,
// capped at 40 characters. An empty result (including an empty input)
// becomes "auto".
func slugify(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	lastWasUnderscore := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastWasUnderscore = false
		case !lastWasUnderscore:
			b.WriteRune('_')
			lastWasUnderscore = true
		}
	}
	result := strings.Trim(b.String(), "_")
	if len(result) > 40 {
		result = strings.Trim(result[:40], "_")
	}
	if result == "" {
		result = "auto"
	}
	return result
}
