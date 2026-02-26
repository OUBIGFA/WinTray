package main

import (
	"os"

	"wintray/internal/app"
)

func main() {
	app.Run(os.Args[1:])
}
