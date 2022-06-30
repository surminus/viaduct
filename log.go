package viaduct

import (
	"fmt"
	"log"

	"github.com/fatih/color"
)

type logger struct {
	// This is the resource type, such as git, file and directory
	Resource string

	// This denotes the action taken on the resource, such as create or delete
	Action string
}

func newLogger(resource, action string) *logger {
	return &logger{
		Resource: resource,
		Action:   action,
	}
}

func (l *logger) Fatal(v ...interface{}) {
	log.Fatalln(loggerOutput(red(l.Resource), red(l.Action), v...))
}

func (l *logger) Info(v ...interface{}) {
	log.Println(loggerOutput(green(l.Resource), green(l.Action), v...))
}

func (l *logger) Warn(v ...interface{}) {
	log.Println(loggerOutput(yellow(l.Resource), yellow(l.Action), v...))
}

func loggerOutput(resource, action string, v ...interface{}) string {
	return fmt.Sprintf("==> %s [%s] %s", resource, action, fmt.Sprint(v...))
}

var green = color.New(color.FgGreen).SprintFunc()
var red = color.New(color.FgRed).SprintFunc()
var yellow = color.New(color.FgYellow).SprintFunc()
