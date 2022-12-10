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
	log.Fatalln(loggerOutput(fatal(l.Resource), fatal(l.Action), v...))
}

// Critical is like Fatal, but without exiting
func (l *logger) Critical(v ...interface{}) {
	log.Println(loggerOutput(critical(l.Resource), critical(l.Action), v...))
}

// Info outputs informational messages
func (l *logger) Info(v ...interface{}) {
	log.Println(loggerOutput(info(l.Resource), info(l.Action), v...))
}

// Warn prints warning messages
func (l *logger) Warn(v ...interface{}) {
	log.Println(loggerOutput(warn(l.Resource), warn(l.Action), v...))
}

// Noop prints no operation messages
func (l *logger) Noop(v ...interface{}) {
	log.Println(loggerOutput(noop(l.Resource), noop(fmt.Sprintf("%s (%s)", l.Action, "up-to-date")), v...))
}

func loggerOutput(resource, action string, v ...interface{}) string {
	return fmt.Sprintf("==> %s [%s] %s", resource, action, fmt.Sprint(v...))
}

var critical = color.New(color.FgRed).SprintFunc()
var fatal = color.New(color.FgRed).SprintFunc()
var info = color.New(color.FgGreen).SprintFunc()
var noop = color.New(color.FgBlue, color.Faint).SprintFunc()
var warn = color.New(color.FgYellow).SprintFunc()
