package viaduct

import "log"

var Attribute Attributes
var Config Configs

func init() {
	log.Println("Initialising attributes...")
	InitAttributes(&Attribute)
	InitConfigs(&Config)

	if Config.DryRun {
		log.Println("WARNING: dry run mode enabled")
	}
}
