package viaduct

import (
	"fmt"
	"log"
	"os"

	"github.com/fatih/color"
)

// Logger provides Viaduct log output.
type Logger struct {
	// This is the resource type, such as git, file and directory
	Resource string

	// This denotes the action taken on the resource, such as create or delete
	Action string

	// Quiet disables all output other than errors
	Quiet bool

	// Silent disables all output
	Silent bool
}

func NewLogger(resource, action string) *Logger {
	return &Logger{
		Resource: resource,
		Action:   action,
	}
}

func NewQuietLogger(resource, action string) *Logger {
	return &Logger{
		Resource: resource,
		Action:   action,
		Quiet:    true,
	}
}

func NewSilentLogger() *Logger {
	return &Logger{Silent: true}
}

func (l *Logger) Fatal(v ...interface{}) {
	if l.Silent {
		os.Exit(1)
	}

	log.Fatalln(LoggerOutput(fatal(l.Resource), fatal(l.Action), v...))
}

// Critical is like Fatal, but without exiting
func (l *Logger) Critical(v ...interface{}) {
	if l.Silent {
		return
	}

	log.Println(LoggerOutput(critical(l.Resource), critical(l.Action), v...))
}

// Info outputs informational messages
func (l *Logger) Info(v ...interface{}) {
	if l.Silent || l.Quiet {
		return
	}

	log.Println(LoggerOutput(info(l.Resource), info(l.Action), v...))
}

// Warn prints warning messages
func (l *Logger) Warn(v ...interface{}) {
	if l.Silent || l.Quiet {
		return
	}

	log.Println(LoggerOutput(warn(l.Resource), warn(l.Action), v...))
}

// Noop prints no operation messages
func (l *Logger) Noop(v ...interface{}) {
	if l.Silent || l.Quiet {
		return
	}

	log.Println(LoggerOutput(noop(l.Resource), noop(fmt.Sprintf("%s (%s)", l.Action, "up-to-date")), v...))
}

func LoggerOutput(resource, action string, v ...interface{}) string {
	return fmt.Sprintf("==> %s [%s] %s", resource, action, fmt.Sprint(v...))
}

var critical = color.New(color.FgRed).SprintFunc()
var fatal = color.New(color.FgRed).SprintFunc()
var info = color.New(color.FgGreen).SprintFunc()
var noop = color.New(color.FgBlue, color.Faint).SprintFunc()
var warn = color.New(color.FgYellow).SprintFunc()
