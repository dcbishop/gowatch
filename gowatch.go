package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"

	"gopkg.in/fatih/color.v0"
	"gopkg.in/fsnotify.v1"
)

func main() {
	Main(os.Stdout, os.Stderr)
}

func clear(eout io.Writer) {
	cmd := exec.Command("clear")
	cmd.Stdout = os.Stdout

	err := cmd.Run()
	if err != nil {
		fmt.Fprintln(eout, err)
	}
}

// Builder contains a running building process.
type Builder struct {
	buildCmd ReusableCommand
	testCmd  ReusableCommand

	buildOut io.Reader
	testOut  io.Reader
}

// NewBuilder make a new builder.
func NewBuilder() Builder {
	builder := Builder{}

	builder.buildCmd = ReusableCommand{
		Name:   "Build",
		Args:   []string{"go", "build", "./..."},
		Output: make(chan CommandResult),
	}

	builder.testCmd = ReusableCommand{
		Name:   "Test",
		Args:   []string{"go", "test", "-v", "./..."},
		Output: make(chan CommandResult),
	}

	return builder
}

// Start the build.
func (builder *Builder) Start() {
	builder.Kill()
	builder.buildCmd.Start()
	builder.testCmd.Start()
}

// Kill the build.
func (builder *Builder) Kill() {
	builder.testCmd.Kill()
	builder.buildCmd.Kill()
}

// ReusableCommand stores a command to execute, if it is started again while the last execution is still running it will kill it silently.
type ReusableCommand struct {
	cmd    *exec.Cmd
	lock   sync.Mutex
	Name   string
	Args   []string
	Output chan (CommandResult)
}

// Status of CommandResult
type Status int

// Possible statuses for CommandResults
const (
	StatusDirty Status = iota
	StatusOk
	StatusBad
)

// CommandResult stores the result of a completed ReusableCommand operation.
type CommandResult struct {
	Output string
	Name   string
	Status Status
}

var ok = color.New(color.Bold, color.FgGreen).SprintFunc()
var bad = color.New(color.Bold, color.FgRed).SprintFunc()
var refresh = color.New(color.Bold, color.FgWhite).SprintFunc()
var normal = color.New(color.FgWhite, color.Bold).SprintFunc()
var dim = color.New(color.FgWhite, color.Faint).SprintFunc()

// StatusIcon maps a Status state to a unicode icon.
var StatusIcon = map[Status]string{
	StatusDirty: "⟳",
	StatusOk:    "✔",
	StatusBad:   "✘",
}

func (cr *CommandResult) String() string {
	state := ok
	text := normal
	if cr.Status == StatusBad {
		state = bad
	} else if cr.Status == StatusDirty {
		state = refresh
		text = dim
	}

	return state(cr.Name+" "+StatusIcon[cr.Status]) + normal(": ") + text(cr.Output)
}

// Start begins executing the command.
func (mcmd *ReusableCommand) Start() {
	mcmd.Kill()

	mcmd.lock.Lock()
	go func() {
		cmd := mcmd.cmd

		var outBuf bytes.Buffer
		cmd.Stdout = &outBuf

		err := cmd.Start()
		mcmd.lock.Unlock()

		err = cmd.Wait()

		cr := CommandResult{
			Output: outBuf.String(),
			Name:   mcmd.Name,
			Status: StatusOk,
		}

		if err != nil {
			// Don't output anything is the command was killed.
			if WasKilled(err) {
				return
			}

			cr.Status = StatusBad
		}

		mcmd.Output <- cr
	}()
}

// WasKilled will check an error as returned by Command.Wait and return true if it was killed.
func WasKilled(err error) bool {
	switch e := err.(type) {
	case *exec.ExitError:
		switch se := e.Sys().(type) {
		case syscall.WaitStatus:
			if se.Signal() == syscall.SIGKILL {
				return true
			}
		default:
			panic("LINUX ONLY")
		}
	}
	return false
}

// Kill the running command.
func (mcmd *ReusableCommand) Kill() {
	{
		mcmd.lock.Lock()
		if mcmd.cmd != nil && mcmd.cmd.Process != nil {
			mcmd.cmd.Process.Kill()
		}
		mcmd.lock.Unlock()
	}
	mcmd.reset()
}

func (mcmd *ReusableCommand) reset() {
	mcmd.lock.Lock()
	defer mcmd.lock.Unlock()
	mcmd.cmd = exec.Command(mcmd.Args[0], mcmd.Args[1:]...)
}

func display(out io.Writer, bRes, tRes CommandResult) {
	clear(out)
	fmt.Fprintln(out, bRes.String())
	fmt.Fprintln(out, tRes.String())
}

// Main function
func Main(out io.Writer, eout io.Writer) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}

	done := make(chan bool)

	builder := NewBuilder()

	var bRes CommandResult
	var tRes CommandResult

	go func() {
		for {
			select {
			case ev := <-watcher.Events:
				if !strings.HasSuffix(ev.Name, ".go") {
					continue
				}
				builder.Start()

				tRes.Status = StatusDirty
				bRes.Status = StatusDirty
			case err := <-watcher.Errors:
				fmt.Fprintln(eout, "error:", err)
			case op := <-builder.testCmd.Output:
				tRes = op
			case op := <-builder.buildCmd.Output:
				bRes = op
			}
			display(out, bRes, tRes)
		}
	}()

	builder.Start()

	err = watcher.Add(".")
	if err != nil {
		log.Fatal(err)
	}

	<-done

	watcher.Close()
	return nil
}
