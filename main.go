package viaduct

import (
	"fmt"
	"log"
	"os"
)

var (
	Attribute Attributes
	Config    Configs

	tmpDirPath string
)

func init() {
	initAttributes(&Attribute)
	initConfigs(&Config)

	if Config.OutputAttributes {
		fmt.Println(Attribute.JSON())
		os.Exit(0)
	}

	if Config.DryRun {
		log.Println("WARNING: dry run mode enabled")
	}
}
