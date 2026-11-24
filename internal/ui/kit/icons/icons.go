// Package icons embeds the Solar Linear assets used by the program manager.
// Only the stroke color is set to currentColor; see ../../icons/LICENSE.solar.
package icons

import (
	"embed"
	"github.com/egoist/mygo/ui"
)

//go:embed solar/*.svg
var files embed.FS

func load(name string) *ui.SVG {
	data, err := files.ReadFile("solar/" + name + ".svg")
	if err != nil {
		panic(err)
	}
	return ui.MustParseSVG(data)
}

var (
	WindowFrame  = load("window-frame")
	Add          = load("add")
	FolderOpen   = load("folder-open")
	TrashBin     = load("trash-bin-trash")
	Play         = load("play")
	Stop         = load("stop")
	Eye          = load("eye")
	Minimize     = load("minimize-square-minimalistic")
	List         = load("list")
	Rocket       = load("rocket")
	Calendar     = load("calendar")
	Stopwatch    = load("stopwatch")
	Translate    = load("translate")
	Settings     = load("settings")
	AltArrowDown = load("alt-arrow-down")
	Check        = load("check")
	Loader       = load("loader")
)
