package analyze

import (
	"github.com/tork-go/orm/gen/ast"
	"github.com/tork-go/orm/gen/token"
)

// pendingRelation is one relation field waiting for its counterpart.
// The field lists stay as raw identifiers here because fields: may
// name a column declared later in the model and references: a column
// in another file; both resolve only after every model is filled.
type pendingRelation struct {
	field *Field
	attr  *ast.Attribute

	relName  string
	nameSpan token.Span

	fieldsArg []ast.Ident
	hasFields bool
	refsArg   []ast.Ident
	hasRefs   bool

	onDelete string
	onUpdate string

	through     *Model
	throughSpan token.Span
	hasThrough  bool

	fkName string
}

// span anchors diagnostics about the relation as a whole: the
// attribute when one was written, the field name otherwise.
func (p *pendingRelation) span() token.Span {
	if p.attr != nil {
		return p.attr.Span
	}
	return p.field.Decl.Name.Span
}

var relationArgNames = []string{"fields", "references", "onDelete", "onUpdate", "through", "map"}
var actionNames = []string{"Cascade", "Restrict", "NoAction", "SetNull", "SetDefault"}

// parseRelationAttr reads @relation's arguments into a pending record.
// Shape mistakes are reported here; meaning is judged in the
// resolution pass, when both sides are on the table.
func (a *analyzer) parseRelationAttr(m *Model, f *Field, attr *ast.Attribute) *pendingRelation {
	p := &pendingRelation{field: f, attr: attr}
	positionals := 0
	for _, arg := range attr.Args {
		if arg.Name == nil {
			positionals++
			if positionals > 1 {
				a.errorf(m.File, arg.Span, "@relation takes one positional argument, its name")
				continue
			}
			lit, ok := arg.Value.(*ast.StringLit)
			if !ok {
				if !isBad(arg.Value) {
					a.errorf(m.File, spanOf(arg.Value), `the relation name must be a string, e.g. @relation("UserPosts")`)
				}
				continue
			}
			p.relName = lit.Value
			p.nameSpan = lit.Span
			continue
		}
		switch arg.Name.Name {
		case "fields":
			idents, ok := a.identList(m.File, "fields: expects a list of field names, e.g. fields: [authorId]", arg.Value)
			if ok {
				p.fieldsArg = idents
				p.hasFields = true
			}
		case "references":
			idents, ok := a.identList(m.File, "references: expects a list of field names, e.g. references: [id]", arg.Value)
			if ok {
				p.refsArg = idents
				p.hasRefs = true
			}
		case "onDelete", "onUpdate":
			action, ok := a.actionArg(m.File, arg)
			if !ok {
				continue
			}
			if arg.Name.Name == "onDelete" {
				p.onDelete = action
			} else {
				p.onUpdate = action
			}
		case "through":
			id, ok := arg.Value.(*ast.Ident)
			if !ok {
				if !isBad(arg.Value) {
					a.errorf(m.File, spanOf(arg.Value), "through: expects a model name, e.g. through: PostTag")
				}
				continue
			}
			target := a.models[id.Name]
			if target == nil {
				a.errorf(m.File, id.Span, "through: names an unknown model %q%s", id.Name, suggestion(id.Name, a.modelNames()))
				continue
			}
			p.through = target
			p.throughSpan = id.Span
			p.hasThrough = true
		case "map":
			value, ok := a.namedString(m.File, "@relation", arg)
			if !ok {
				continue
			}
			if !isSQLIdent(value) {
				a.errorf(m.File, spanOf(arg.Value), "map: value %q is not a valid identifier", value)
				continue
			}
			p.fkName = value
		default:
			a.errorf(m.File, arg.Name.Span, "unknown argument %q in @relation%s", arg.Name.Name, suggestion(arg.Name.Name, relationArgNames))
		}
	}
	return p
}

func (a *analyzer) actionArg(file string, arg *ast.AttrArg) (string, bool) {
	id, ok := arg.Value.(*ast.Ident)
	if !ok {
		if !isBad(arg.Value) {
			a.errorf(file, spanOf(arg.Value), "invalid %s action (use Cascade, Restrict, NoAction, SetNull, or SetDefault)", arg.Name.Name)
		}
		return "", false
	}
	for _, action := range actionNames {
		if id.Name == action {
			return action, true
		}
	}
	a.errorf(file, id.Span, "invalid %s action %q (use Cascade, Restrict, NoAction, SetNull, or SetDefault)%s",
		arg.Name.Name, id.Name, suggestion(id.Name, actionNames))
	return "", false
}

func (a *analyzer) modelNames() []string {
	names := make([]string, 0, len(a.modelOrder))
	for _, m := range a.modelOrder {
		names = append(names, m.Name)
	}
	return names
}

// relKey pairs the two sides of a relation: the model pair, unordered,
// plus the @relation name. Two unnamed relations between the same two
// models land on one key with four fields, which is exactly the
// ambiguity the diagnostic then names.
type relKey struct {
	low, high string
	name      string
}

func pairKey(p *pendingRelation) relKey {
	owner := p.field.Model.Name
	target := p.field.Type.Model.Name
	if owner > target {
		owner, target = target, owner
	}
	return relKey{low: owner, high: target, name: p.relName}
}

// resolveRelations pairs every relation field with its counterpart and
// fills in the semantic Relation records. It runs after all models are
// filled, so both sides and every referenced column exist by now.
func (a *analyzer) resolveRelations() {
	throughModels := map[*Model]bool{}
	for _, p := range a.pending {
		if p.hasThrough {
			throughModels[p.through] = true
		}
	}
	groups := map[relKey][]*pendingRelation{}
	var order []relKey
	for _, p := range a.pending {
		k := pairKey(p)
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], p)
	}
	for _, k := range order {
		group := groups[k]
		if k.low == k.high && k.name == "" {
			for _, p := range group {
				a.errorf(p.field.Model.File, p.span(), `a self relation must be named: @relation("Name", ...)`)
			}
			continue
		}
		switch len(group) {
		case 1:
			p := group[0]
			if p.hasFields && throughModels[p.field.Model] {
				// A join model's belongs to sides need no inverse:
				// the endpoints already reach across it with their
				// through: relation, and forcing PostTag[] fields on
				// them would defeat the point of the sugar.
				a.resolveJoinBelongsTo(k, p)
				continue
			}
			target := p.field.Type.Model
			owner := p.field.Model
			hint := ""
			if k.name != "" {
				hint = " with @relation(" + quote(k.name) + ")"
			}
			a.errorf(owner.File, p.span(), "field %q has no matching relation field on model %q; add one of type %s or %s[]%s",
				p.field.Name, target.Name, owner.Name, owner.Name, hint)
		case 2:
			a.pairRelation(k, group[0], group[1])
		default:
			for _, p := range group {
				if k.name == "" {
					a.errorf(p.field.Model.File, p.span(), `ambiguous relations between %q and %q; name each pair with @relation("Name")`, k.low, k.high)
				} else {
					a.errorf(p.field.Model.File, p.span(), "relation %q is declared by more than two fields", k.name)
				}
			}
		}
	}
}

// pairRelation validates one two sided relation and writes the
// Relation records both fields carry from here on.
func (a *analyzer) pairRelation(k relKey, p, q *pendingRelation) {
	if p.hasThrough || q.hasThrough {
		a.pairManyToMany(k, p, q)
		return
	}
	if p.field.List && q.field.List {
		a.errorf(p.field.Model.File, p.span(), "many to many requires through: naming the join model on both sides")
		a.errorf(q.field.Model.File, q.span(), "many to many requires through: naming the join model on both sides")
		return
	}
	switch {
	case p.hasFields && q.hasFields:
		a.errorf(p.field.Model.File, p.span(), "fields: and references: belong on one side of the relation only")
		return
	case !p.hasFields && !q.hasFields:
		a.errorf(p.field.Model.File, p.span(), "one side of the relation between %q and %q must declare fields: and references:", p.field.Model.Name, q.field.Model.Name)
		return
	}
	owner, inverse := p, q
	if q.hasFields {
		owner, inverse = q, p
	}
	if inverse.hasRefs {
		a.errorf(inverse.field.Model.File, inverse.span(), "references: also needs fields: on the same side")
		return
	}
	if inverse.onDelete != "" || inverse.onUpdate != "" {
		a.errorf(inverse.field.Model.File, inverse.span(), "onDelete:/onUpdate: belong on the side that declares fields:")
		return
	}
	if inverse.fkName != "" {
		a.errorf(inverse.field.Model.File, inverse.span(), "map: belongs on the side that declares fields:")
		return
	}
	fks, refs, ok := a.resolveOwnerKeys(owner)
	if !ok {
		return
	}

	targetModel := owner.field.Type.Model
	ownerModel := owner.field.Model
	owner.field.Relation = &Relation{
		Kind:       RelBelongsTo,
		RelName:    k.name,
		Target:     targetModel,
		Inverse:    inverse.field,
		Fields:     fks,
		References: refs,
		OnDelete:   owner.onDelete,
		OnUpdate:   owner.onUpdate,
		FKName:     owner.fkName,
	}
	inverseKind := RelHasOne
	if inverse.field.List {
		inverseKind = RelHasMany
	}
	inverse.field.Relation = &Relation{
		Kind:       inverseKind,
		RelName:    k.name,
		Target:     ownerModel,
		Inverse:    owner.field,
		Fields:     fks,
		References: refs,
	}
}

// resolveJoinBelongsTo resolves a join model's one sided belongs to.
// It gets a Relation with no Inverse; codegen still emits its foreign
// key and Via wiring, which is all a join model needs.
func (a *analyzer) resolveJoinBelongsTo(k relKey, p *pendingRelation) {
	fks, refs, ok := a.resolveOwnerKeys(p)
	if !ok {
		return
	}
	p.field.Relation = &Relation{
		Kind:       RelBelongsTo,
		RelName:    k.name,
		Target:     p.field.Type.Model,
		Fields:     fks,
		References: refs,
		OnDelete:   p.onDelete,
		OnUpdate:   p.onUpdate,
		FKName:     p.fkName,
	}
}

// resolveOwnerKeys validates an owning side's fields: and references:
// and resolves both lists to columns. Everything that can be wrong
// with a foreign key pairing is judged here, shared by ordinary
// belongs to sides and a join model's one sided ones.
func (a *analyzer) resolveOwnerKeys(owner *pendingRelation) (fks, refs []*Field, ok bool) {
	file := owner.field.Model.File
	if !owner.hasRefs {
		a.errorf(file, owner.span(), "@relation with fields: also needs references:")
		return nil, nil, false
	}
	if owner.field.List {
		a.errorf(file, owner.span(), "the side that declares fields: must be singular, not a list")
		return nil, nil, false
	}
	if len(owner.fieldsArg) != len(owner.refsArg) {
		a.errorf(file, owner.span(), "fields: and references: have different lengths")
		return nil, nil, false
	}
	if len(owner.fieldsArg) == 0 {
		a.errorf(file, owner.span(), "fields: expects a list of field names, e.g. fields: [authorId]")
		return nil, nil, false
	}
	ownerModel := owner.field.Model
	targetModel := owner.field.Type.Model
	for i, id := range owner.fieldsArg {
		fk := ownerModel.FieldNamed(id.Name)
		if fk == nil {
			a.errorf(file, id.Span, "unknown field %q in fields:%s", id.Name, suggestion(id.Name, ownerModel.columnFieldNames()))
			return nil, nil, false
		}
		if reason := fkIneligibility(fk); reason != "" {
			a.errorf(file, id.Span, "%q in fields: cannot be a %s field", id.Name, reason)
			return nil, nil, false
		}
		refID := owner.refsArg[i]
		ref := targetModel.FieldNamed(refID.Name)
		if ref == nil {
			a.errorf(file, refID.Span, "model %q has no field %q (referenced in references:)%s", targetModel.Name, refID.Name, suggestion(refID.Name, targetModel.columnFieldNames()))
			return nil, nil, false
		}
		if reason := fkIneligibility(ref); reason != "" {
			a.errorf(file, refID.Span, "%q in references: cannot be a %s field", refID.Name, reason)
			return nil, nil, false
		}
		if fk.Type.Kind != ref.Type.Kind || fk.Type.Enum != ref.Type.Enum {
			a.errorf(file, id.Span, "foreign key %q (%s) does not match referenced %q (%s)", fk.Name, typeDisplay(fk), ref.Name, typeDisplay(ref))
			return nil, nil, false
		}
		switch {
		case owner.field.Optional && !fk.Optional:
			a.errorf(file, id.Span, "field %q is optional but its foreign key %q is required; make both optional or both required", owner.field.Name, fk.Name)
			return nil, nil, false
		case !owner.field.Optional && fk.Optional:
			a.errorf(file, id.Span, "field %q is required but its foreign key %q is optional; make both optional or both required", owner.field.Name, fk.Name)
			return nil, nil, false
		}
		fks = append(fks, fk)
		refs = append(refs, ref)
	}
	if owner.onDelete == "SetNull" {
		for _, fk := range fks {
			if !fk.Optional {
				a.errorf(file, owner.span(), "onDelete: SetNull requires optional foreign key fields")
				return nil, nil, false
			}
		}
	}
	if !referencesUniqueSet(targetModel, refs) {
		a.warningf(file, owner.span(), "references: columns do not form the primary key or a unique index on %q", targetModel.Name)
	}
	return fks, refs, true
}

// fkIneligibility names why a field cannot serve as a foreign key
// column or its referenced counterpart.
func fkIneligibility(f *Field) string {
	switch {
	case f.Type.Kind == TypeModel:
		return "relation"
	case f.Type.Kind == TypeJson:
		return "Json"
	case f.List:
		return "list"
	}
	return ""
}

// referencesUniqueSet reports whether the referenced columns are
// guaranteed unique on the target: its primary key, a single @unique
// column, or a unique index over exactly that set.
func referencesUniqueSet(target *Model, refs []*Field) bool {
	if sameFieldSet(target.PrimaryKey, refs) {
		return true
	}
	if len(refs) == 1 && refs[0].Unique {
		return true
	}
	for _, idx := range target.Indexes {
		if idx.Unique && len(idx.Expressions) == 0 && sameFieldSet(idx.Fields, refs) {
			return true
		}
	}
	return false
}

func sameFieldSet(a, b []*Field) bool {
	if len(a) == 0 || len(a) != len(b) {
		return false
	}
	set := map[*Field]bool{}
	for _, f := range a {
		set[f] = true
	}
	for _, f := range b {
		if !set[f] {
			return false
		}
	}
	return true
}

// pairManyToMany validates a through relation and resolves the join
// model's two foreign keys.
func (a *analyzer) pairManyToMany(k relKey, p, q *pendingRelation) {
	if !p.field.List || !q.field.List {
		a.errorf(p.field.Model.File, p.span(), "through: is only for many to many relations (both sides must be lists)")
		return
	}
	if k.low == k.high {
		a.errorf(p.field.Model.File, p.span(), "many to many between a model and itself is not supported; model the join with two has many relations")
		return
	}
	if !p.hasThrough || !q.hasThrough || p.through != q.through {
		a.errorf(p.field.Model.File, p.span(), "both sides must agree on the same through: model")
		a.errorf(q.field.Model.File, q.span(), "both sides must agree on the same through: model")
		return
	}
	for _, side := range []*pendingRelation{p, q} {
		if side.hasFields || side.hasRefs {
			a.errorf(side.field.Model.File, side.span(), "fields:/references: do not apply to many to many relations (the join model owns the keys)")
			return
		}
		if side.onDelete != "" || side.onUpdate != "" {
			a.errorf(side.field.Model.File, side.span(), "onDelete:/onUpdate: do not apply to many to many relations (set them on the join model)")
			return
		}
	}
	join := p.through
	pKey, ok := a.joinKey(join, p.field.Model, p.throughSpan, p.field.Model.File)
	if !ok {
		return
	}
	qKey, ok := a.joinKey(join, q.field.Model, p.throughSpan, p.field.Model.File)
	if !ok {
		return
	}
	p.field.Relation = &Relation{
		Kind: RelManyToMany, RelName: k.name,
		Target: q.field.Model, Inverse: q.field,
		Through: join, ThroughLocal: pKey, ThroughForeign: qKey,
	}
	q.field.Relation = &Relation{
		Kind: RelManyToMany, RelName: k.name,
		Target: p.field.Model, Inverse: p.field,
		Through: join, ThroughLocal: qKey, ThroughForeign: pKey,
	}
}

// joinKey finds the join model's foreign key column pointing at one
// endpoint: the first fields: column of its belongs to relation to
// that model. Exactly one such relation may exist, or the join is
// ambiguous.
func (a *analyzer) joinKey(join, endpoint *Model, span token.Span, file string) (*Field, bool) {
	var found *Field
	for _, p := range a.pending {
		if p.field.Model != join || p.field.Type.Model != endpoint || !p.hasFields || len(p.fieldsArg) == 0 {
			continue
		}
		if found != nil {
			a.errorf(file, span, "join model %q has more than one relation to %q; many to many needs exactly one", join.Name, endpoint.Name)
			return nil, false
		}
		found = join.FieldNamed(p.fieldsArg[0].Name)
	}
	if found == nil {
		a.errorf(file, span, "join model %q needs a belongs to relation to %q (with fields: and references:)", join.Name, endpoint.Name)
		return nil, false
	}
	return found, true
}
