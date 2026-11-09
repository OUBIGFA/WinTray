// Package icons embeds the Lucide assets used by the program manager.
// The original SVG files are unchanged; see ../../icons/LICENSE.lucide.
package icons

import (
	"embed"
	"github.com/egoist/mygo/ui"
)

//go:embed lucide/*.svg
var files embed.FS

func load(name string) *ui.SVG {
	data, err := files.ReadFile("lucide/" + name + ".svg")
	if err != nil {
		panic(err)
	}
	return ui.MustParseSVG(data)
}

var (
	AppWindow     = load("app-window")
	Plus          = load("plus")
	FolderOpen    = load("folder-open")
	Trash2        = load("trash-2")
	Play          = load("play")
	Square        = load("square")
	PanelTop      = load("panel-top")
	PanelTopClose = load("panel-top-close")
	List          = load("list")
	Power         = load("power")
	Rocket        = load("rocket")
	CalendarClock = load("calendar-clock")
	Timer         = load("timer")
	Languages     = load("languages")
	Settings2     = load("settings-2")
	ChevronDown   = load("chevron-down")
	Check         = load("check")
	LoaderCircle  = load("loader-circle")
	GripVertical  = load("grip-vertical")
)
