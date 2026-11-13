package kube

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Problem is one finding of CheckManifest, positioned for the editor.
type Problem struct {
	Line, Column int    // 1-based line, 0-based rune column.
	Path         string // spec.replicas
	Message      string
}

var yamlErrorLine = regexp.MustCompile(`line (\d+):`)

// CheckManifest validates the first YAML document against the schema:
// unknown fields, type mismatches, enum violations and missing required
// fields. A document that does not parse yields one problem at the parse
// error's line. Problems are sorted by line.
func CheckManifest(doc []byte, s *Schema) []Problem {
	var root yaml.Node
	if err := yaml.Unmarshal(doc, &root); err != nil {
		line := 1
		if m := yamlErrorLine.FindStringSubmatch(err.Error()); m != nil {
			line, _ = strconv.Atoi(m[1])
		}
		msg := err.Error()
		msg = strings.TrimPrefix(msg, "yaml: ")
		return []Problem{{Line: line, Message: msg}}
	}
	if s == nil || root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return nil
	}
	c := &checker{seen: map[*yaml.Node]bool{}}
	c.check(root.Content[0], s, "", nil)
	sort.SliceStable(c.problems, func(i, j int) bool {
		if c.problems[i].Line != c.problems[j].Line {
			return c.problems[i].Line < c.problems[j].Line
		}
		return c.problems[i].Column < c.problems[j].Column
	})
	return c.problems
}

type checker struct {
	problems []Problem
	seen     map[*yaml.Node]bool
}

func (c *checker) add(n *yaml.Node, path, msg string) {
	c.problems = append(c.problems, Problem{Line: n.Line, Column: max(0, n.Column-1), Path: path, Message: msg})
}

func joinPath(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

// key is the mapping key that introduced n (nil at the root); a missing
// required field is reported on that key's line.
func (c *checker) check(n *yaml.Node, s *Schema, path string, key *yaml.Node) {
	if n == nil || s == nil {
		return
	}
	for n.Kind == yaml.AliasNode && n.Alias != nil {
		n = n.Alias
	}
	if c.seen[n] {
		return // An alias cycle or a shared anchor already checked.
	}
	c.seen[n] = true
	defer delete(c.seen, n)
	if n.Kind == yaml.ScalarNode && n.Tag == "!!null" {
		if s.Nullable || s.Type == "" {
			return
		}
		c.add(n, path, "null is not allowed here")
		return
	}
	switch s.Type {
	case "object":
		if n.Kind != yaml.MappingNode {
			c.add(n, path, "expected an object (key: value lines)")
			return
		}
		present := map[string]bool{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			key := k.Value
			if k.Kind == yaml.ScalarNode && k.Tag == "!!merge" {
				continue
			}
			present[key] = true
			sub, ok := s.Properties[key]
			switch {
			case ok:
				c.check(v, sub, joinPath(path, key), k)
			case s.Additional != nil:
				c.check(v, s.Additional, joinPath(path, key), k)
			case s.AdditionalOK || s.PreserveUnknown:
			default:
				c.add(k, joinPath(path, key), "unknown field "+strconv.Quote(key))
			}
		}
		var missing []string
		for _, req := range s.Required {
			if !present[req] {
				missing = append(missing, req)
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			where := path
			if where == "" {
				where = "the document"
			}
			at := n
			if key != nil {
				at = key
			}
			c.add(at, path, "missing required field(s) in "+where+": "+strings.Join(missing, ", "))
		}
	case "array":
		if n.Kind != yaml.SequenceNode {
			c.add(n, path, "expected a list (- items)")
			return
		}
		for i, item := range n.Content {
			c.check(item, s.Items, path+"["+strconv.Itoa(i)+"]", nil)
		}
	case "string", "integer", "number", "boolean":
		if n.Kind != yaml.ScalarNode {
			c.add(n, path, "expected a "+s.Type+" value")
			return
		}
		c.checkScalar(n, s, path)
	default:
		// No type: anything goes, but nested properties, when given, are
		// still checked (int-or-string and preserve-unknown objects).
		if s.IntOrString {
			if n.Kind != yaml.ScalarNode {
				c.add(n, path, "expected an integer or a string")
				return
			}
			c.checkEnum(n, s, path)
			return
		}
		if n.Kind == yaml.MappingNode && len(s.Properties) > 0 {
			sub := *s
			sub.Type = "object"
			c.check(n, &sub, path, key)
		}
	}
}

func (c *checker) checkScalar(n *yaml.Node, s *Schema, path string) {
	value := n.Value
	quoted := n.Style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle) != 0 || n.Tag == "!!str" && n.Style != 0
	switch s.Type {
	case "integer":
		if s.IntOrString {
			break
		}
		if _, err := strconv.ParseInt(value, 0, 64); err != nil || quoted {
			c.add(n, path, "expected an integer, got "+strconv.Quote(value))
			return
		}
	case "number":
		if _, err := strconv.ParseFloat(value, 64); err != nil || quoted {
			c.add(n, path, "expected a number, got "+strconv.Quote(value))
			return
		}
	case "boolean":
		if (value != "true" && value != "false") || quoted {
			c.add(n, path, "expected true or false, got "+strconv.Quote(value))
			return
		}
	case "string":
		if !quoted && n.Tag != "!!str" && n.Tag != "" && n.Tag != "!!null" {
			// Unquoted numbers and booleans are not strings in YAML.
			if n.Tag == "!!int" || n.Tag == "!!float" || n.Tag == "!!bool" {
				c.add(n, path, "expected a string; quote the value "+strconv.Quote(value))
				return
			}
		}
	}
	c.checkEnum(n, s, path)
}

func (c *checker) checkEnum(n *yaml.Node, s *Schema, path string) {
	if len(s.Enum) == 0 {
		return
	}
	for _, e := range s.Enum {
		if e == n.Value {
			return
		}
	}
	c.add(n, path, "must be one of: "+strings.Join(s.Enum, ", "))
}

// Completion is one suggestion of CompleteManifest.
type Completion struct {
	Insert   string // Text to insert after the typed prefix; structural for objects and arrays.
	Label    string // The name, with " *" when the field is required.
	Detail   string // Type and short description.
	Type     string // Rendered type: integer, object, []object, string (enum: a, b).
	Required bool
}

// FieldContext describes the field the cursor line belongs to (ContextAt).
type FieldContext struct {
	Path        string // e.g. spec.replicas
	Name        string // Key of the cursor line ("" on a blank or bare "- " line).
	Type        string // Rendered type.
	Required    bool   // Required in its parent.
	Enum        []string
	Description string   // First sentence.
	Children    []string // Object types: property names, required first and marked with " *".
	InValue     bool     // The cursor is after "key: ", typing a value.
}

// CompleteManifest suggests keys of the mapping at the cursor or values of
// the key on the cursor's line. The context comes from the text alone
// (indentation), so a line being typed need not parse.
func CompleteManifest(doc []byte, line, column int, s *Schema) (string, []Completion) {
	if s == nil {
		return "", nil
	}
	lines := strings.Split(strings.ReplaceAll(string(doc), "\r\n", "\n"), "\n")
	if line < 1 || line > len(lines) {
		return "", nil
	}
	current := []rune(lines[line-1])
	column = max(0, min(column, len(current)))
	before := string(current[:column])
	// Value position: "key: pre|" with a key already on this line.
	if key, val, ok := splitKeyValue(before); ok {
		keyIndent := indentOf(before)
		parent, siblings := mappingContext(lines, line-1, keyIndent, s)
		if parent == nil {
			return "", nil
		}
		if parent.Type == "array" && parent.Items != nil {
			parent = parent.Items // "- key: value": the key belongs to the item.
		}
		key = strings.TrimLeft(strings.TrimPrefix(strings.TrimLeft(key, " "), "-"), " ")
		field := parent.Properties[strings.TrimSpace(key)]
		if field == nil && parent.Additional != nil {
			field = parent.Additional
		}
		if field == nil {
			return "", nil
		}
		_ = siblings
		prefix := strings.TrimLeft(val, " ")
		var candidates []string
		switch {
		case len(field.Enum) > 0:
			candidates = field.Enum
		case field.Type == "boolean":
			candidates = []string{"true", "false"}
		}
		var items []Completion
		for _, v := range candidates {
			if strings.HasPrefix(v, prefix) {
				items = append(items, Completion{Insert: v[len(prefix):], Label: v, Detail: detail(field), Type: typeName(field)})
			}
		}
		return prefix, items
	}
	// Key position: the word before the cursor on a line that is only
	// indentation (and maybe "- ") plus the word.
	text := before
	indent := indentOf(text)
	rest := strings.TrimLeft(text, " ")
	item := false
	if strings.HasPrefix(rest, "- ") || rest == "-" {
		item = true
		rest = strings.TrimLeft(strings.TrimPrefix(rest, "-"), " ")
		indent = len(text) - len(rest)
	}
	if strings.ContainsAny(rest, " :") {
		return "", nil
	}
	prefix := rest
	var mapping *Schema
	var present map[string]bool
	if item {
		parent, _ := mappingContext(lines, line-1, indentOf(text), s)
		if parent == nil || parent.Type != "array" || parent.Items == nil {
			return "", nil
		}
		mapping = parent.Items
		present = map[string]bool{}
	} else {
		mapping, present = mappingContext(lines, line-1, indent, s)
		if mapping == nil {
			return "", nil
		}
		if mapping.Type == "array" {
			// Continuing a sequence item's mapping on its own line.
			mapping = mapping.Items
			if mapping == nil {
				return "", nil
			}
		}
	}
	if mapping.Type != "object" && len(mapping.Properties) == 0 {
		return "", nil
	}
	var names []string
	for name := range mapping.Properties {
		if !present[name] && strings.HasPrefix(name, prefix) {
			names = append(names, name)
		}
	}
	required := map[string]bool{}
	for _, r := range mapping.Required {
		required[r] = true
	}
	sort.Slice(names, func(i, j int) bool {
		if required[names[i]] != required[names[j]] {
			return required[names[i]]
		}
		return names[i] < names[j]
	})
	childIndent := strings.Repeat(" ", indent+2)
	var items []Completion
	for _, name := range names {
		field := mapping.Properties[name]
		insert := name[len(prefix):] + ": "
		switch field.Type {
		case "object":
			insert = name[len(prefix):] + ":\n" + childIndent
		case "array":
			insert = name[len(prefix):] + ":\n" + childIndent + "- "
		}
		label := name
		if required[name] {
			label += " *"
		}
		items = append(items, Completion{Insert: insert, Label: label, Detail: detail(field), Type: typeName(field), Required: required[name]})
	}
	return prefix, items
}

// ContextAt resolves the field the cursor line belongs to: the key on that
// line under its parent chain, the array's item schema on a "- " line, or
// the parent mapping itself (Name "") on a line without a key. nil when
// nothing resolves.
func ContextAt(doc []byte, line, column int, s *Schema) *FieldContext {
	if s == nil {
		return nil
	}
	lines := strings.Split(strings.ReplaceAll(string(doc), "\r\n", "\n"), "\n")
	if line < 1 || line > len(lines) {
		return nil
	}
	current := lines[line-1]
	text := strings.TrimRight(current, " \t")
	indent := indentOf(text)
	if strings.TrimSpace(current) == "" {
		indent = indentOf(strings.TrimRight(current, "\t\r")) // A blank line keeps its indentation.
	}
	body := strings.TrimLeft(text, " ")
	item := false
	if strings.HasPrefix(body, "- ") || body == "-" {
		item = true
		body = strings.TrimLeft(strings.TrimPrefix(body, "-"), " ")
		indent = len(text) - len(body)
		if body == "" {
			indent = indentOf(text) + 2
		}
	}
	key, _, hasKey := splitKeyValue(body)
	runes := []rune(current)
	column = max(0, min(column, len(runes)))
	inValue := false
	if hasKey {
		if i := strings.Index(current, key+":"); i >= 0 {
			inValue = column > len([]rune(current[:i+len(key)+1]))
		}
	}
	var parent *Schema
	if item {
		// The list is found from the dash, not from the key after it:
		// earlier "- " lines of the same list must not count as parents.
		arr, _ := mappingContext(lines, line-1, indentOf(text), s)
		if arr == nil || arr.Type != "array" || arr.Items == nil {
			return nil
		}
		parent = arr.Items
	} else {
		parent, _ = mappingContext(lines, line-1, indent, s)
		if parent == nil {
			return nil
		}
		if parent.Type == "array" && parent.Items != nil {
			parent = parent.Items
		}
	}
	path := pathAt(lines, line-1, indent, item)
	if !hasKey {
		if parent.Type != "object" && len(parent.Properties) == 0 {
			return nil
		}
		return &FieldContext{Path: path, Type: typeName(parent), Enum: parent.Enum, Description: parent.Summary(), Children: childNames(parent)}
	}
	field := parent.Properties[key]
	if field == nil && parent.Additional != nil {
		field = parent.Additional
	}
	if field == nil {
		return nil
	}
	required := false
	for _, r := range parent.Required {
		required = required || r == key
	}
	ctx := &FieldContext{Path: joinPath(path, key), Name: key, Type: typeName(field), Required: required, Enum: field.Enum, Description: field.Summary(), InValue: inValue}
	if field.Type == "object" || len(field.Properties) > 0 {
		ctx.Children = childNames(field)
	} else if field.Type == "array" && field.Items != nil && (field.Items.Type == "object" || len(field.Items.Properties) > 0) {
		ctx.Children = childNames(field.Items)
	}
	return ctx
}

// pathAt rebuilds the dotted path of the mapping a line belongs to from the
// indentation chain above it ("spec.items" for a line inside an item).
func pathAt(lines []string, row, indent int, item bool) string {
	var parts []string
	want := indent
	for i := row - 1; i >= 0 && want > 0; i-- {
		t := strings.TrimRight(lines[i], " \t")
		if strings.TrimSpace(t) == "" || strings.HasPrefix(strings.TrimSpace(t), "#") {
			continue
		}
		ind := indentOf(t)
		if ind >= want {
			continue
		}
		body := strings.TrimLeft(t, " ")
		if strings.HasPrefix(body, "- ") || body == "-" {
			body = strings.TrimLeft(strings.TrimPrefix(body, "-"), " ")
		}
		if key, _, ok := splitKeyValue(body); ok && strings.TrimSpace(strings.TrimPrefix(body, key+":")) == "" {
			parts = append(parts, key)
		}
		want = ind
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return strings.Join(parts, ".")
}

func childNames(s *Schema) []string {
	required := map[string]bool{}
	for _, r := range s.Required {
		required[r] = true
	}
	var names []string
	for name := range s.Properties {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if required[names[i]] != required[names[j]] {
			return required[names[i]]
		}
		return names[i] < names[j]
	})
	for i, name := range names {
		if required[name] {
			names[i] = name + " *"
		}
	}
	return names
}

// typeName renders a schema's type for completion lists and status lines.
func typeName(s *Schema) string {
	t := s.Type
	switch {
	case s.IntOrString:
		t = "int or string"
	case t == "":
		t = "any"
	case t == "array" && s.Items != nil:
		if s.Items.Type != "" {
			t = "[]" + s.Items.Type
		} else {
			t = "[]any"
		}
	}
	if len(s.Enum) > 0 {
		t += " (enum: " + strings.Join(s.Enum, ", ") + ")"
	}
	return t
}

func detail(s *Schema) string {
	t := typeName(s)
	if d := s.Summary(); d != "" {
		return t + " · " + d
	}
	return t
}

func indentOf(line string) int {
	return len(line) - len(strings.TrimLeft(line, " "))
}

// splitKeyValue recognises "key: value-so-far" on the text before the cursor.
func splitKeyValue(before string) (key, value string, ok bool) {
	rest := strings.TrimLeft(before, " ")
	if strings.HasPrefix(rest, "- ") {
		rest = strings.TrimLeft(rest[1:], " ")
	}
	i := strings.Index(rest, ":")
	if i <= 0 {
		return "", "", false
	}
	key = rest[:i]
	if strings.ContainsAny(key, " \"'") {
		return "", "", false
	}
	after := rest[i+1:]
	if after != "" && !strings.HasPrefix(after, " ") {
		return "", "", false // "a:b" is not a key.
	}
	return key, after, true
}

// context finds the schema of the mapping that a line with the given
// indentation belongs to, by walking up through less indented "key:" lines,
// and the keys already present in that mapping.
func mappingContext(lines []string, row, indent int, root *Schema) (*Schema, map[string]bool) {
	// Collect the chain of parents: nearest preceding line with a smaller
	// indentation that opens a block ("key:" or "- key:" or "-").
	type step struct {
		key    string
		item   bool
		indent int
	}
	var chain []step
	want := indent
	for i := row - 1; i >= 0 && want > 0; i-- {
		l := lines[i]
		t := strings.TrimRight(l, " \t")
		if strings.TrimSpace(t) == "" || strings.HasPrefix(strings.TrimSpace(t), "#") {
			continue
		}
		ind := indentOf(t)
		if ind >= want {
			continue
		}
		body := strings.TrimLeft(t, " ")
		item := false
		if strings.HasPrefix(body, "- ") || body == "-" {
			item = true
			body = strings.TrimLeft(strings.TrimPrefix(body, "-"), " ")
		}
		key, _, ok := splitKeyValue(body)
		if ok && strings.TrimSpace(strings.TrimPrefix(body, key+":")) == "" {
			chain = append(chain, step{key: key, item: item, indent: ind})
			want = ind
			if item {
				// "- key:" opens an item whose mapping holds key; the list
				// itself belongs to the next parent up.
				want = ind
			}
			continue
		}
		if item && body == "" {
			chain = append(chain, step{item: true, indent: ind})
			want = ind
			continue
		}
		if ok && item {
			// "- key: value" starts an item mapping; our line is a sibling key of it.
			chain = append(chain, step{item: true, indent: ind})
			want = ind
			continue
		}
		if ok {
			// A scalar key at a lower indentation: our line is a sibling of it, not a child.
			want = ind
			continue
		}
	}
	// Walk from the root down the chain (which is innermost first).
	s := root
	for i := len(chain) - 1; i >= 0; i-- {
		st := chain[i]
		if st.item {
			if s.Type == "array" && s.Items != nil {
				s = s.Items
			} else if s.Type != "array" && st.key == "" {
				return nil, nil
			}
		}
		if st.key != "" {
			next := s.Properties[st.key]
			if next == nil && s.Additional != nil {
				next = s.Additional
			}
			if next == nil {
				return nil, nil
			}
			s = next
			if st.item && s.Type == "array" {
				// "- key:" inside an array of objects: key belongs to the item.
			}
		}
	}
	if s.Type == "array" && s.Items != nil && len(chain) > 0 && !chain[0].item {
		// Keys under a "key:" that is an array are items; the caller handles "- ".
	}
	present := presentKeys(lines, row, indent)
	return s, present
}

// presentKeys lists keys at exactly the given indentation in the mapping
// around row (the contiguous block with indentation >= indent).
func presentKeys(lines []string, row, indent int) map[string]bool {
	present := map[string]bool{}
	// A "- " line two columns to the left starts a sequence item: scanning
	// up it is our own item's first key (counted, then stop); scanning down
	// it begins the next item (stop before it).
	scan := func(i int, up bool) bool {
		t := strings.TrimRight(lines[i], " \t")
		if strings.TrimSpace(t) == "" {
			return true
		}
		ind := indentOf(t)
		body := strings.TrimLeft(t, " ")
		itemStart := false
		if strings.HasPrefix(body, "- ") || body == "-" {
			if ind == indent-2 {
				if !up {
					return false
				}
				itemStart = true
				body = strings.TrimLeft(body[1:], " ")
				ind = len(t) - len(body)
			} else if ind < indent {
				return false
			}
		}
		if ind < indent {
			return false
		}
		if ind == indent {
			if key, _, ok := splitKeyValue(body); ok {
				present[key] = true
			}
		}
		return !itemStart
	}
	for i := row - 1; i >= 0 && scan(i, true); i-- {
	}
	for i := row + 1; i < len(lines) && scan(i, false); i++ {
	}
	return present
}
