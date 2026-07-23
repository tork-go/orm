package analyze

import (
	"github.com/tork-go/orm/gen/ast"
	"github.com/tork-go/orm/gen/token"
)

// Schema is the resolved semantic model of one schema directory. Models
// and Enums are sorted by name, not by file or declaration order, so
// the same schema content produces the same Schema no matter how it is
// split across files; generated output inherits that determinism.
type Schema struct {
	Datasource Datasource
	Models     []*Model
	Enums      []*Enum
}

// Datasource is the single datasource block of a schema. Its only
// current job is naming the provider, which gates the @db native type
// namespace.
type Datasource struct {
	Name     string
	Provider string
	File     string
	Span     token.Span
}

// Enum is one enum declaration. DBName is the Postgres type name the
// enum creates, derived from the enum name or overridden with @@map.
type Enum struct {
	Name   string
	DBName string
	Values []string
	Doc    string
	File   string
	Decl   *ast.EnumDecl
}

// Model is one model declaration with everything resolved: fields in
// declaration order, the table name after pluralization or @@map, and
// the table level constraints.
type Model struct {
	Name      string
	TableName string
	Doc       string
	Fields    []*Field
	// PrimaryKey lists the fields forming the key, one entry for @id
	// and several for @@id, in the order the key declares them.
	PrimaryKey []*Field
	SoftDelete *Field
	Indexes    []*Index
	Checks     []*Check
	File       string
	Decl       *ast.ModelDecl
}

// Field is one field line, column and relation fields alike. A relation
// field has Relation set and no ColumnName; a column field is the
// reverse. GoName and ColumnName are fixed here rather than in codegen
// because collision checking is a semantic question: two fields mapping
// to one column is a schema error, not a formatting accident.
type Field struct {
	Name       string
	GoName     string
	ColumnName string
	Type       FieldType
	Optional   bool
	List       bool

	IsID    bool
	Unique  bool
	Indexed bool

	Default    *Default
	VarcharLen int
	// NumericPrecision and NumericScale carry @db.Numeric; a zero
	// precision means the attribute was absent, since Postgres does
	// not allow NUMERIC(0, s) anyway.
	NumericPrecision int
	NumericScale     int
	JSONText         bool // @db.Json chose text json over the jsonb default
	GoType           GoTypeRef
	SoftDelete       bool

	Relation *Relation

	Doc   string
	Model *Model
	Decl  *ast.FieldDecl
}

// FieldType is a resolved type reference: a scalar kind, or a pointer
// to the enum or model it named.
type FieldType struct {
	Kind  TypeKind
	Enum  *Enum
	Model *Model
}

// TypeKind enumerates what a field's type can resolve to. The scalar
// kinds mirror the ORM's column vocabulary one to one; TypeEnum,
// TypeJson, and TypeModel are the three that carry extra payload on the
// field.
type TypeKind int

const (
	TypeBoolean TypeKind = iota
	TypeInt
	TypeInt32
	TypeBigInt
	TypeFloat
	TypeDouble
	TypeDecimal
	TypeString
	TypeDateTime
	TypeUuid
	TypeJson
	TypeEnum
	TypeModel
)

// typeNames spells each kind the way schema files do, so diagnostics
// and hovers quote the user's own vocabulary.
var typeNames = [...]string{
	TypeBoolean:  "Boolean",
	TypeInt:      "Int",
	TypeInt32:    "Int32",
	TypeBigInt:   "BigInt",
	TypeFloat:    "Float",
	TypeDouble:   "Double",
	TypeDecimal:  "Decimal",
	TypeString:   "String",
	TypeDateTime: "DateTime",
	TypeUuid:     "Uuid",
	TypeJson:     "Json",
	TypeEnum:     "enum",
	TypeModel:    "model",
}

// String names the kind as spelled in schema files.
func (k TypeKind) String() string { return typeNames[k] }

// Default is a field's @default, already shaped for emission. Exactly
// one interpretation applies per kind: SQL carries the server side
// rendering for now(), dbgenerated(), and literals; GoFunc carries the
// client side generator for uuid() and go(); autoincrement carries
// nothing because the ORM derives identity rather than declaring it.
type Default struct {
	Kind DefaultKind
	// SQL is the exact server default expression handed to the ORM's
	// ServerDefault, quoting already applied.
	SQL    string
	GoFunc GoTypeRef
	Span   token.Span
}

// DefaultKind separates the @default forms that generate differently.
type DefaultKind int

const (
	DefaultAutoincrement DefaultKind = iota
	DefaultNow
	DefaultDBGenerated
	DefaultLiteral
	DefaultUUID
	DefaultGoFunc
)

// GoTypeRef names a Go type or function the schema refers to, split
// into the import path and the bare identifier. An empty ImportPath
// means the identifier lives in the generated package itself, which is
// how a schema binds a Json field to a type the user wrote by hand next
// to the generated files.
type GoTypeRef struct {
	ImportPath string
	Name       string
}

// Index is one @@index or @@unique, resolved to fields. Expressions
// holds raw SQL key expressions from the on: argument, which have no
// field to resolve to by design.
type Index struct {
	Fields      []*Field
	Expressions []string
	Unique      bool
	Name        string
	Where       string
	Decl        *ast.BlockAttribute
}

// Check is one @@check constraint.
type Check struct {
	Expression string
	Name       string
	Decl       *ast.BlockAttribute
}

// Relation is the resolved half of a relation, hung off the field that
// declared it. Kind decides which ORM marker the generator emits, and
// the key columns are resolved fields on whichever side owns them.
type Relation struct {
	Kind RelationKind
	// RelName is the @relation("...") pairing label, empty when the
	// model pair was unambiguous without one.
	RelName string
	// Target is the model on the other side; Inverse is its paired
	// field over there. Inverse is nil for a join model's belongs to
	// sides, which need no counterpart because the endpoints reach
	// across the join with their through: relation instead.
	Target  *Model
	Inverse *Field
	// Fields and References are the foreign key pairing. On a belongs
	// to side, Fields live on the declaring model and References on
	// the target. The has many and has one sides carry the same two
	// slices seen from across the table, because emitting their Via
	// wiring needs the owner's key column too.
	Fields     []*Field
	References []*Field
	// Through is the join model of a many to many relation, with the
	// join model's two foreign key columns: ThroughLocal references
	// the declaring side, ThroughForeign the target side.
	Through        *Model
	ThroughLocal   *Field
	ThroughForeign *Field
	OnDelete       string
	OnUpdate       string
	// FKName is the optional map: constraint name override.
	FKName string
}

// RelationKind mirrors the ORM's four relationship markers.
type RelationKind int

const (
	RelBelongsTo RelationKind = iota
	RelHasMany
	RelHasOne
	RelManyToMany
)
