package viaduct

import (
	"fmt"
	"log"
	"os"
)

var (
	Attribute SystemAttributes
	Cli       CliFlags

	tmpDirPath string
)

func init() {
	initAttributes(&Attribute)
	initCli(&Cli)

	if Cli.Attributes {
		fmt.Println(Attribute.JSON())
		os.Exit(0)
	}

	if Cli.DryRun {
		log.Println("WARNING: dry run mode enabled")
	}
}
