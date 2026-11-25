package aispaces

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

// An outline is what an agent reads instead of a whole file: the headings of
// a document or the declarations of a source file, with line numbers. It lets
// the agent ask for one section rather than pay for the file.

type heading struct {
	line, level int
	title       string
}

var (
	markdownHeading = regexp.MustCompile(`^(#{1,6})\s+(\S.*)$`)
	// Declarations are recognised by line shape, not parsed: good enough to
	// navigate, cheap for every language.
	declarations = map[string]*regexp.Regexp{
		".go":    regexp.MustCompile(`^(func|type|var|const)\b`),
		".py":    regexp.MustCompile(`^\s*(class|def|async\s+def)\b`),
		".js":    regexp.MustCompile(`^(export\s+)?(default\s+)?(async\s+)?(function|class|const|let|var)\b`),
		".ts":    regexp.MustCompile(`^(export\s+)?(default\s+)?(declare\s+)?(async\s+)?(abstract\s+)?(function|class|const|let|var|interface|type|enum|namespace)\b`),
		".rs":    regexp.MustCompile(`^\s{0,4}(pub(\([a-z]+\))?\s+)?(async\s+)?(fn|struct|enum|trait|impl|mod|type|const|static)\b`),
		".java":  regexp.MustCompile(`^\s{0,4}(public|private|protected|static|final|abstract|class|interface|enum|record)\b.*[({]`),
		".swift": regexp.MustCompile(`^\s{0,4}(public|private|internal|open|fileprivate|final|static|override)?\s*(func|class|struct|enum|protocol|extension|init)\b`),
		".rb":    regexp.MustCompile(`^\s*(class|module|def)\b`),
		".sh":    regexp.MustCompile(`^(function\s+)?[A-Za-z_][A-Za-z0-9_]*\s*\(\)`),
		".c":     regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_ \t\*]*\([^;]*$|^(struct|enum|union|typedef)\b`),
	}
	sameAs = map[string]string{
		".jsx": ".js", ".mjs": ".js", ".cjs": ".js", ".tsx": ".ts", ".kt": ".java", ".cs": ".java", ".scala": ".java",
		".h": ".c", ".cc": ".c", ".cpp": ".c", ".hpp": ".c", ".m": ".c", ".mm": ".c", ".bash": ".sh", ".zsh": ".sh", ".php": ".java",
	}
)

func isMarkdown(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".md", ".markdown", ".mdx":
		return true
	}
	return false
}

// outlineOf lists the headings or declarations of a text file.
func outlineOf(name string, lines []string) []heading {
	var out []heading
	if isMarkdown(name) {
		fenced := false
		for i, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "```") {
				fenced = !fenced
				continue
			}
			if m := markdownHeading.FindStringSubmatch(line); m != nil && !fenced {
				out = append(out, heading{i + 1, len(m[1]), strings.TrimSpace(m[2])})
			}
		}
		return out
	}
	ext := strings.ToLower(path.Ext(name))
	if other, ok := sameAs[ext]; ok {
		ext = other
	}
	re := declarations[ext]
	if re == nil {
		return nil
	}
	for i, line := range lines {
		if !re.MatchString(line) {
			continue
		}
		level := 1
		if line != strings.TrimLeft(line, " \t") {
			level = 2
		}
		title := strings.TrimRight(strings.TrimSpace(line), " {:")
		if len(title) > 110 {
			title = cut(title, 110) + "…"
		}
		out = append(out, heading{i + 1, level, title})
	}
	return out
}

// cut shortens s to at most n bytes without splitting a UTF-8 sequence.
func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && s[n]&0xC0 == 0x80 {
		n--
	}
	return s[:n]
}

// sectionRange finds the part of a file that a heading or declaration
// starts: up to the next one of the same or a higher level.
func sectionRange(entries []heading, total int, want string) (first, last int, title string, err error) {
	want = strings.ToLower(strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(want), "#")))
	found := -1
	for i, e := range entries {
		t := strings.ToLower(e.title)
		if t == want {
			found = i
			break
		}
		if found < 0 && strings.Contains(t, want) {
			found = i
		}
	}
	if found < 0 || want == "" {
		var known []string
		for i, e := range entries {
			if i == 40 {
				known = append(known, "…")
				break
			}
			known = append(known, fmt.Sprintf("%d: %s", e.line, e.title))
		}
		if len(known) == 0 {
			return 0, 0, "", fmt.Errorf("not_found: this file has no headings or declarations to select; use offset and limit")
		}
		return 0, 0, "", fmt.Errorf("not_found: no section %q. Sections:\n%s", want, strings.Join(known, "\n"))
	}
	e := entries[found]
	last = total
	for _, next := range entries[found+1:] {
		if next.level <= e.level {
			last = next.line - 1
			break
		}
	}
	return e.line, last, e.title, nil
}

func short(size int64) string {
	switch {
	case size < 1000:
		return fmt.Sprintf("%dB", size)
	case size < 10_000:
		return fmt.Sprintf("%.1fK", float64(size)/1000)
	case size < 1_000_000:
		return fmt.Sprintf("%dK", size/1000)
	}
	return fmt.Sprintf("%.1fM", float64(size)/1e6)
}

const outlineBudget = 220 // lines returned for a folder

// outline describes one file in full, or every text file below a folder
// within a fixed budget: the deeper levels are dropped first.
func outline(fs *FS, p string) (string, error) {
	p = clean(p)
	info, err := fs.Lstat(p)
	if err != nil {
		return "", err
	}
	load := func(file string) ([]string, bool) {
		data, truncated, err := fs.ReadFile(file, 2<<20)
		if err != nil || truncated || strings.ContainsRune(string(data[:min(len(data), 4096)]), 0) {
			return nil, false
		}
		return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n"), true
	}
	if !info.IsDir() {
		lines, ok := load(p)
		if !ok {
			return "", fmt.Errorf("%s is not a text file of up to 2 MiB", p)
		}
		entries := outlineOf(p, lines)
		var out strings.Builder
		fmt.Fprintf(&out, "%s — %d lines, %s\n", p, len(lines), short(info.Size()))
		if len(entries) == 0 {
			out.WriteString("(no headings or declarations found; read it with offset and limit)\n")
		}
		for i, e := range entries {
			if i == 400 {
				fmt.Fprintf(&out, "… %d more\n", len(entries)-i)
				break
			}
			fmt.Fprintf(&out, "%d: %s%s\n", e.line, strings.Repeat("  ", e.level-1), markdownMark(p, e))
		}
		out.WriteString("[read one part with vfs_read section=\"<title>\"]")
		return out.String(), nil
	}
	type fileOutline struct {
		rel     string
		lines   int
		entries []heading
	}
	var files []fileOutline
	skipped, visited := 0, 0
	var walk func(string) error
	walk = func(d string) error {
		list, err := fs.ReadDir(d)
		if err != nil {
			return err
		}
		for _, item := range list {
			full := path.Join(d, item.Name())
			if item.IsDir() {
				if item.Name() != ".git" && item.Name() != "node_modules" {
					if err := walk(full); err != nil {
						return err
					}
				}
				continue
			}
			if visited++; visited > 3000 || !item.Mode().IsRegular() {
				continue
			}
			ext := strings.ToLower(path.Ext(item.Name()))
			if other, ok := sameAs[ext]; ok {
				ext = other
			}
			if !isMarkdown(item.Name()) && declarations[ext] == nil {
				skipped++
				continue
			}
			if lines, ok := load(full); ok {
				files = append(files, fileOutline{strings.TrimPrefix(strings.TrimPrefix(full, p), "/"), len(lines), outlineOf(full, lines)})
			}
		}
		return nil
	}
	if err = walk(p); err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "no documents or source files here; use vfs_list", nil
	}
	// Try ever tighter filters until the result fits the budget.
	for _, limit := range []struct{ level, perFile int }{{2, 30}, {2, 10}, {1, 8}, {1, 3}, {0, 0}} {
		var out []string
		for _, f := range files {
			out = append(out, fmt.Sprintf("%s (%d lines)", f.rel, f.lines))
			shown, more := 0, 0
			for _, e := range f.entries {
				if e.level > limit.level {
					continue
				}
				if shown == limit.perFile {
					more++
					continue
				}
				shown++
				out = append(out, fmt.Sprintf("  %d: %s%s", e.line, strings.Repeat("  ", e.level-1), markdownMark(f.rel, e)))
			}
			if more > 0 {
				out = append(out, fmt.Sprintf("  … %d more", more))
			}
		}
		if len(out) <= outlineBudget || limit.level == 0 {
			if len(out) > outlineBudget {
				out = append(out[:outlineBudget], fmt.Sprintf("… %d more lines: outline a subfolder", len(out)-outlineBudget))
			}
			note := fmt.Sprintf("[outline of %s: %d files", p, len(files))
			if skipped > 0 {
				note += fmt.Sprintf("; %d other files are listed by vfs_list", skipped)
			}
			return note + ". Read one part with vfs_read section=\"<title>\"]\n" + strings.Join(out, "\n"), nil
		}
	}
	return "", nil
}

func markdownMark(name string, e heading) string {
	if isMarkdown(name) {
		return strings.Repeat("#", e.level) + " " + e.title
	}
	return e.title
}
