package migrate

import (
	"fmt"
	"strings"

	"github.com/tork-go/orm/driver"
)

// Generate renders ops into one SQL string via dialect: one statement per
// Render* call, statements joined with a blank line between them and each
// terminated with a semicolon.
func Generate(dialect driver.Dialect, ops []Operation) (string, error) {
	var stmts []string
	for _, op := range ops {
		s, err := renderOp(dialect, op)
		if err != nil {
			return "", err
		}
		stmts = append(stmts, s...)
	}

	var b strings.Builder
	for i, s := range stmts {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(s)
		b.WriteString(";")
	}
	return b.String(), nil
}

// renderOp dispatches one Operation to its matching Render* method on
// dialect.
func renderOp(dialect driver.Dialect, op Operation) ([]string, error) {
	switch o := op.(type) {
	case CreateTable:
		return dialect.RenderCreateTable(o.Table)
	case DropTable:
		return dialect.RenderDropTable(o.Table), nil
	case AddColumn:
		return dialect.RenderAddColumn(o.Table, o.Column)
	case DropColumn:
		return dialect.RenderDropColumn(o.Table, o.Column), nil
	case AlterColumnType:
		return dialect.RenderAlterColumnType(o.Table, o.Column)
	case AlterColumnDefault:
		return dialect.RenderAlterColumnDefault(o.Table, o.Column, o.Default), nil
	case AlterColumnNullability:
		return dialect.RenderAlterColumnNullability(o.Table, o.Column, o.NotNull), nil
	case AddPrimaryKey:
		return dialect.RenderAddPrimaryKey(o.Table, o.PrimaryKey), nil
	case DropPrimaryKey:
		return dialect.RenderDropPrimaryKey(o.Table, o.Name), nil
	case AddUnique:
		return dialect.RenderAddUnique(o.Table, o.Unique), nil
	case DropUnique:
		return dialect.RenderDropUnique(o.Table, o.Name), nil
	case AddIndex:
		return dialect.RenderAddIndex(o.Table, o.Index), nil
	case DropIndex:
		return dialect.RenderDropIndex(o.Table, o.Name), nil
	case AddCheck:
		return dialect.RenderAddCheck(o.Table, o.Check), nil
	case DropCheck:
		return dialect.RenderDropCheck(o.Table, o.Name), nil
	case AddForeignKey:
		return dialect.RenderAddForeignKey(o.Table, o.ForeignKey), nil
	case DropForeignKey:
		return dialect.RenderDropForeignKey(o.Table, o.Name), nil
	case CreateEnumType:
		return dialect.RenderCreateEnumType(o.Enum), nil
	case DropEnumType:
		return dialect.RenderDropEnumType(o.Name), nil
	case AddEnumValue:
		return dialect.RenderAddEnumValue(o.Name, o.Value, o.Before, o.After), nil
	default:
		return nil, fmt.Errorf("migrate: unknown operation type %T", op)
	}
}
