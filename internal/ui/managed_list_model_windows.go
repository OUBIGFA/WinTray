//go:build windows

package ui

import "github.com/lxn/walk"

type managedListRow struct {
	Name    string
	Path    string
	Mode    string
	Enabled bool
}

// managedListTableModel shows each program with its own icon and a check box
// for starting it at sign-in. Programs that are switched off are greyed out.
type managedListTableModel struct {
	walk.TableModelBase
	rows      []managedListRow
	onChecked func(row int, checked bool)
}

func newManagedListTableModel(onChecked func(row int, checked bool)) *managedListTableModel {
	return &managedListTableModel{rows: make([]managedListRow, 0), onChecked: onChecked}
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
		return m.rows[row].Mode
	default:
		return ""
	}
}

func (m *managedListTableModel) Checked(row int) bool {
	return row >= 0 && row < len(m.rows) && m.rows[row].Enabled
}

func (m *managedListTableModel) SetChecked(row int, checked bool) error {
	if row < 0 || row >= len(m.rows) {
		return nil
	}
	m.rows[row].Enabled = checked
	if m.onChecked != nil {
		m.onChecked(row, checked)
	}
	return nil
}

// Image returns the program's file path, from which the list shows the icon
// Explorer would show for it.
func (m *managedListTableModel) Image(row int) any {
	if row < 0 || row >= len(m.rows) || m.rows[row].Path == "" {
		return nil
	}
	return m.rows[row].Path
}

func (m *managedListTableModel) StyleCell(style *walk.CellStyle) {
	if row := style.Row(); row >= 0 && row < len(m.rows) && !m.rows[row].Enabled {
		style.TextColor = secondaryColor
	}
}

func (m *managedListTableModel) SetRows(rows []managedListRow) {
	m.rows = rows
	m.PublishRowsReset()
}

func (m *managedListTableModel) SetRow(row int, value managedListRow) {
	if row < 0 || row >= len(m.rows) {
		return
	}
	m.rows[row] = value
	m.PublishRowChanged(row)
}
