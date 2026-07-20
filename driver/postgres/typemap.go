package postgres

import (
	"fmt"

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
	default:
		return "", fmt.Errorf("postgres: no type mapping for column kind %v", t.Kind)
	}
}

// parseType maps an information_schema.columns data_type value, plus
// character_maximum_length for varchar columns, back to a schema.ColumnType.
func parseType(dataType string, charMaxLen *int) (schema.ColumnType, error) {
	switch dataType {
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
		if charMaxLen != nil {
			length = *charMaxLen
		}
		return schema.ColumnType{Kind: schema.KindVarchar, Length: length}, nil
	case "text":
		return schema.ColumnType{Kind: schema.KindText}, nil
	case "timestamp without time zone":
		return schema.ColumnType{Kind: schema.KindTimestamp}, nil
	default:
		return schema.ColumnType{}, fmt.Errorf("postgres: no column kind for data_type %q", dataType)
	}
}
