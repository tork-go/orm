package analyze

// fillEnum resolves one enum declaration: its values, and the only
// block attribute an enum accepts, @@map. Enum values carry no other
// syntax, so this pass is short by nature.
func (a *analyzer) fillEnum(e *Enum) {
	e.Doc = e.Decl.Doc.Text()
	seen := map[string]bool{}
	for _, v := range e.Decl.Values {
		if seen[v.Name.Name] {
			a.errorf(e.File, v.Name.Span, "enum value %q repeated in enum %q", v.Name.Name, e.Name)
			continue
		}
		seen[v.Name.Name] = true
		e.Values = append(e.Values, v.Name.Name)
	}
	if len(e.Values) == 0 {
		a.errorf(e.File, e.Decl.Name.Span, "enum %q has no values", e.Name)
	}
	for _, attr := range e.Decl.Attrs {
		if attr.Name.Name != "map" {
			a.errorf(e.File, attr.Span, "only @@map is allowed inside an enum")
			continue
		}
		if name, ok := a.mapName(e.File, "@@map", "type_name", attr.Span, attr.Args); ok {
			e.DBName = name
		}
	}
}
