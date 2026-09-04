package tui

import (
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// openPicker opens the directory browser for the Default local root: an existing
// value is shown inside its parent with that folder highlighted, else the
// nearest existing ancestor (or home) is listed. Reuses the shared dirPicker.
func (s *settingsModel) openPicker() tea.Cmd {
	dir, highlight := pickerStart(s.inputs[defaultLocalRootIdx()].Value())
	p := dirPicker{dir: dir, height: s.pickerHeight(), focus: focusList}
	p.entries = readSubdirs(dir)
	p.cursor = indexOf(p.entries, highlight)
	p.ensureVisible()
	s.picker = p
	s.browsing = true
	return nil
}

// updatePicker drives the open browser, mirroring formModel.updatePicker: list
// focus moves/opens/ascends and space selects; button focus toggles Select/
// Cancel; esc cancels throughout.
func (s settingsModel) updatePicker(msg tea.Msg) (settingsModel, tea.Cmd) {
	if s.picker.naming {
		return s, s.picker.updateNewDir(msg)
	}
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return s, nil
	}
	if s.picker.focus == focusList {
		switch k.String() {
		case "w", "up":
			s.picker.moveUp()
		case "s", "down":
			s.picker.moveDown()
		case "pgup":
			s.picker.moveBy(-5)
		case "pgdown":
			s.picker.moveBy(5)
		case " ":
			return s.confirmSelection()
		case "enter", "d", "right":
			s.picker.open()
		case "a", "left":
			s.picker.upDir()
		case "n":
			return s, s.picker.startNewDir()
		case "tab":
			s.picker.focusNext()
		case "shift+tab":
			s.picker.focusPrev()
		case "esc":
			s.browsing = false
		}
		return s, nil
	}
	switch k.String() {
	case "tab":
		s.picker.focusNext()
	case "shift+tab":
		s.picker.focusPrev()
	case "left", "a", "right", "d":
		if s.picker.focus == focusSelect {
			s.picker.focus = focusCancel
		} else {
			s.picker.focus = focusSelect
		}
	case "enter", " ":
		if s.picker.focus == focusSelect {
			return s.confirmSelection()
		}
		s.browsing = false // Cancel button
	case "esc":
		s.browsing = false
	}
	return s, nil
}

// confirmSelection writes the chosen folder into the Default local root field
// and closes the picker: the highlighted sub-directory, or the current directory
// when it has none. Focus returns to the field's input.
func (s settingsModel) confirmSelection() (settingsModel, tea.Cmd) {
	sel := s.picker.dir
	if len(s.picker.entries) > 0 {
		sel = filepath.Join(s.picker.dir, s.picker.entries[s.picker.cursor])
	}
	field := defaultLocalRootIdx()
	s.inputs[field].SetValue(sel)
	s.browsing = false
	cmd := s.setFocus(s.slotIndexForField(field))
	return s, cmd
}

// slotIndexForField returns the slot index of field's text input.
func (s settingsModel) slotIndexForField(field int) int {
	for i, sl := range s.slots() {
		if sl.kind == stInput && sl.field == field {
			return i
		}
	}
	return 0
}

// pickerHeight is the number of directory rows listed, sized to the terminal and
// clamped so the modal fits small screens. Mirrors formModel.pickerHeight.
func (s settingsModel) pickerHeight() int {
	h := s.termHeight - 11
	if h > 18 {
		h = 18
	}
	if h < 4 {
		h = 4
	}
	return h
}

// pickerView frames the directory browser as a modal matching the settings width.
func (s settingsModel) pickerView() string {
	inner := s.modalWidth() - 4 // modal borders (2) + body Padding(0,1) (2)
	body := lipgloss.NewStyle().Padding(0, 1).Render(s.picker.view(inner))
	return titledBox("Select default local root", body, s.modalWidth(), lipgloss.Height(body)+2, true)
}
