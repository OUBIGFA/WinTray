package main

import (
	"os"

	"wintray/internal/app"
)

func main() {
	os.Exit(app.Run(os.Args[1:]))
}
