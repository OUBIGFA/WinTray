//go:build windows

package ui

import "github.com/lxn/walk"

type managedListRow struct {
	Name  string
	Path  string
	Param string
}

type managedListTableModel struct {
	walk.TableModelBase
	rows []managedListRow
}

func newManagedListTableModel() *managedListTableModel {
	return &managedListTableModel{rows: make([]managedListRow, 0)}
}

func (m *managedListTableModel) RowCount() int {
	return len(m.rows)
}

func (m *managedListTableModel) Value(row, col int) any {
	if row < 0 || row >= len(m.rows) {
		return ""
	}
	switch col {
	case 0:
		return m.rows[row].Name
	case 1:
		return m.rows[row].Path
	case 2:
		return m.rows[row].Param
	default:
		return ""
	}
}

func (m *managedListTableModel) SetRows(rows []managedListRow) {
	m.rows = rows
	m.PublishRowsReset()
}
