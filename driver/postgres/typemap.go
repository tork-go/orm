package postgres

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/tork-go/orm/schema"
)

// renderType returns the Postgres type string for a schema.ColumnType.
func renderType(t schema.ColumnType) (string, error) {
	switch t.Kind {
	case schema.KindBoolean:
		return "BOOLEAN", nil
	case schema.KindInteger:
		return "INTEGER", nil
	case schema.KindBigInteger:
		return "BIGINT", nil
	case schema.KindFloat:
		return "REAL", nil
	case schema.KindDouble:
		return "DOUBLE PRECISION", nil
	case schema.KindVarchar:
		return fmt.Sprintf("VARCHAR(%d)", t.Length), nil
	case schema.KindText:
		return "TEXT", nil
	case schema.KindTimestamp:
		return "TIMESTAMP WITHOUT TIME ZONE", nil
	case schema.KindUUID:
		return "UUID", nil
	case schema.KindNumeric:
		if t.Precision > 0 {
			return fmt.Sprintf("NUMERIC(%d,%d)", t.Precision, t.Scale), nil
		}
		return "NUMERIC", nil
	case schema.KindJSON:
		return "JSON", nil
	case schema.KindJSONB:
		return "JSONB", nil
	case schema.KindEnum:
		if t.TypeName == "" {
			return "", fmt.Errorf("postgres: enum column has no type name")
		}
		return quoteIdent(t.TypeName), nil
	case schema.KindArray:
		if t.Elem == nil {
			return "", fmt.Errorf("postgres: array column has no element type")
		}
		elemType, err := renderType(*t.Elem)
		if err != nil {
			return "", err
		}
		return elemType + "[]", nil
	default:
		return "", fmt.Errorf("postgres: no type mapping for column kind %v", t.Kind)
	}
}

// rawColumnType holds the raw information_schema.columns fields needed to
// resolve a column's final schema.ColumnType. udt_name is only meaningful
// for a USER-DEFINED data_type (an enum's own bare name).
type rawColumnType struct {
	DataType         string
	UDTName          string
	CharMaxLen       *int
	NumericPrecision *int
	NumericScale     *int
}

// parseType maps a rawColumnType back to a schema.ColumnType, for every
// data_type except "ARRAY" and "USER-DEFINED": those need, respectively,
// a separate format_type() lookup and a validated enum type name, so
// introspectColumns handles them itself before falling back to parseType.
func parseType(raw rawColumnType) (schema.ColumnType, error) {
	switch raw.DataType {
	case "boolean":
		return schema.ColumnType{Kind: schema.KindBoolean}, nil
	case "integer":
		return schema.ColumnType{Kind: schema.KindInteger}, nil
	case "bigint":
		return schema.ColumnType{Kind: schema.KindBigInteger}, nil
	case "real":
		return schema.ColumnType{Kind: schema.KindFloat}, nil
	case "double precision":
		return schema.ColumnType{Kind: schema.KindDouble}, nil
	case "character varying":
		length := 0
		if raw.CharMaxLen != nil {
			length = *raw.CharMaxLen
		}
		return schema.ColumnType{Kind: schema.KindVarchar, Length: length}, nil
	case "text":
		return schema.ColumnType{Kind: schema.KindText}, nil
	case "timestamp without time zone":
		return schema.ColumnType{Kind: schema.KindTimestamp}, nil
	case "uuid":
		return schema.ColumnType{Kind: schema.KindUUID}, nil
	case "numeric":
		ct := schema.ColumnType{Kind: schema.KindNumeric}
		if raw.NumericPrecision != nil {
			ct.Precision = *raw.NumericPrecision
		}
		if raw.NumericScale != nil {
			ct.Scale = *raw.NumericScale
		}
		return ct, nil
	case "json":
		return schema.ColumnType{Kind: schema.KindJSON}, nil
	case "jsonb":
		return schema.ColumnType{Kind: schema.KindJSONB}, nil
	default:
		return schema.ColumnType{}, fmt.Errorf("postgres: no column kind for data_type %q", raw.DataType)
	}
}

// parseArrayFormatType parses the fixed shapes Tork's own renderType ever
// produces for an array column, as returned by Postgres's format_type()
// builtin: "<base>[]", "<base>(n)[]", or "<base>(p,s)[]". This is a
// bounded parsing problem, not general SQL-type parsing: Tork controls
// exactly what it writes for an array column, it never needs to parse an
// arbitrary array type from a hand-built database.
func parseArrayFormatType(s string) (schema.ColumnType, error) {
	if !strings.HasSuffix(s, "[]") {
		return schema.ColumnType{}, fmt.Errorf("postgres: %q is not an array type", s)
	}
	elem, err := parseFormattedBaseType(strings.TrimSuffix(s, "[]"))
	if err != nil {
		return schema.ColumnType{}, err
	}
	return schema.ColumnType{Kind: schema.KindArray, Elem: &elem}, nil
}

// parseFormattedBaseType parses one base type as rendered by
// format_type(), e.g. "character varying(50)", "numeric(10,2)", "integer".
func parseFormattedBaseType(s string) (schema.ColumnType, error) {
	name, args := s, ""
	if i := strings.IndexByte(s, '('); i >= 0 && strings.HasSuffix(s, ")") {
		name, args = s[:i], s[i+1:len(s)-1]
	}

	switch name {
	case "boolean":
		return schema.ColumnType{Kind: schema.KindBoolean}, nil
	case "integer":
		return schema.ColumnType{Kind: schema.KindInteger}, nil
	case "bigint":
		return schema.ColumnType{Kind: schema.KindBigInteger}, nil
	case "real":
		return schema.ColumnType{Kind: schema.KindFloat}, nil
	case "double precision":
		return schema.ColumnType{Kind: schema.KindDouble}, nil
	case "character varying":
		length := 0
		if args != "" {
			n, err := strconv.Atoi(args)
			if err != nil {
				return schema.ColumnType{}, fmt.Errorf("postgres: invalid varchar length in %q", s)
			}
			length = n
		}
		return schema.ColumnType{Kind: schema.KindVarchar, Length: length}, nil
	case "text":
		return schema.ColumnType{Kind: schema.KindText}, nil
	case "timestamp without time zone":
		return schema.ColumnType{Kind: schema.KindTimestamp}, nil
	case "uuid":
		return schema.ColumnType{Kind: schema.KindUUID}, nil
	case "numeric":
		ct := schema.ColumnType{Kind: schema.KindNumeric}
		if args != "" {
			parts := strings.SplitN(args, ",", 2)
			p, err := strconv.Atoi(strings.TrimSpace(parts[0]))
			if err != nil {
				return schema.ColumnType{}, fmt.Errorf("postgres: invalid numeric precision in %q", s)
			}
			ct.Precision = p
			if len(parts) == 2 {
				sc, err := strconv.Atoi(strings.TrimSpace(parts[1]))
				if err != nil {
					return schema.ColumnType{}, fmt.Errorf("postgres: invalid numeric scale in %q", s)
				}
				ct.Scale = sc
			}
		}
		return ct, nil
	default:
		return schema.ColumnType{}, fmt.Errorf("postgres: unrecognized array element type %q", s)
	}
}

// parseAction maps an information_schema.referential_constraints
// update_rule/delete_rule value back to a schema.ForeignKeyAction.
func parseAction(s string) (schema.ForeignKeyAction, error) {
	switch s {
	case "NO ACTION":
		return schema.ActionNoAction, nil
	case "CASCADE":
		return schema.ActionCascade, nil
	case "SET NULL":
		return schema.ActionSetNull, nil
	case "SET DEFAULT":
		return schema.ActionSetDefault, nil
	case "RESTRICT":
		return schema.ActionRestrict, nil
	default:
		return 0, fmt.Errorf("postgres: unrecognized referential action %q", s)
	}
}
