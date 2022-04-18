package viaduct

import (
	"fmt"
	"log"
	"os"
)

var (
	Attribute Attributes
	Config    Configs
)

func init() {
	InitAttributes(&Attribute)
	InitConfigs(&Config)

	if Config.OutputAttributes {
		fmt.Println(Attribute.JSON())
		os.Exit(0)
	}

	if Config.DryRun {
		log.Println("WARNING: dry run mode enabled")
	}
}
