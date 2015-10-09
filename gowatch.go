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
	buildCmd MyCommand
	testCmd  MyCommand

	buildOut io.Reader
	testOut  io.Reader
}

// Start the build.
func (builder *Builder) Start() {
	builder.Kill()
	builder.buildCmd.Start()
	builder.testCmd.Start()
}

// MyCommand stores a command to execute, if it is started again while the last execution is still running it will kill it silently.
type MyCommand struct {
	Cmd    *exec.Cmd
	Lock   sync.Mutex
	Name   string
	Args   []string
	Output chan (CommandResult)
}

var ok = color.New(color.Bold, color.FgGreen).SprintFunc()
var bad = color.New(color.Bold, color.FgRed).SprintFunc()
var refresh = color.New(color.Bold, color.FgWhite).SprintFunc()

// CommandResult stores the result of a completed MyCommand operation.
type CommandResult struct {
	output string
	name   string
	status string
}

func (cr *CommandResult) String() string {
	return cr.name + " " + cr.status + ": " + cr.output
}

// Start begins executing the command.
func (mcmd *MyCommand) Start() {
	mcmd.Kill()

	mcmd.Lock.Lock()
	go func() {
		cmd := mcmd.Cmd

		var outBuf bytes.Buffer
		cmd.Stdout = &outBuf

		err := cmd.Start()
		mcmd.Lock.Unlock()

		err = cmd.Wait()

		cr := CommandResult{
			output: outBuf.String(),
			name:   mcmd.Name,
			status: ok("✔"),
		}

		if err != nil {
			// Don't output anything is the command was killed.
			if WasKilled(err) {
				return
			}

			// Sometimes a command fails with error 1 but works next go, maybe files still being written to disk?
			if ExitStatus(err) == 1 {
				return
			}

			cr.status = bad("✘")
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

// ExitStatus will check an error as returned by Command.Wait and return the exit code.
func ExitStatus(err error) int {
	switch e := err.(type) {
	case *exec.ExitError:
		switch se := e.Sys().(type) {
		case syscall.WaitStatus:
			return se.ExitStatus()
		default:
			panic("LINUX ONLY")
		}
	}

	return 0
}

// Kill the running command.
func (mcmd *MyCommand) Kill() {
	{
		mcmd.Lock.Lock()
		if mcmd.Cmd != nil && mcmd.Cmd.Process != nil {
			mcmd.Cmd.Process.Kill()
		}
		mcmd.Lock.Unlock()
	}
	mcmd.reset()
}

// Kill the build.
func (builder *Builder) Kill() {
	builder.testCmd.Kill()
	builder.buildCmd.Kill()
}

func (mcmd *MyCommand) reset() {
	mcmd.Lock.Lock()
	defer mcmd.Lock.Unlock()
	mcmd.Cmd = exec.Command(mcmd.Args[0], mcmd.Args[1:]...)
}

// NewBuilder make a new builder.
func NewBuilder() Builder {
	builder := Builder{}

	builder.buildCmd = MyCommand{
		Name:   "Build",
		Args:   []string{"go", "build", "./..."},
		Output: make(chan CommandResult),
	}

	builder.testCmd = MyCommand{
		Name:   "Test",
		Args:   []string{"go", "test", "-v", "./..."},
		Output: make(chan CommandResult),
	}

	return builder
}

func display(out io.Writer, bRes, tRes CommandResult, bDirty, tDirty bool) {
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

	bDirty := true
	tDirty := true
	go func() {
		for {
			select {
			case ev := <-watcher.Events:
				if !strings.HasSuffix(ev.Name, ".go") {
					continue
				}
				builder.Start()

				bDirty = true
				tDirty = true
				tRes.status = refresh("⟳")
				bRes.status = refresh("⟳")
			case err := <-watcher.Errors:
				fmt.Fprintln(eout, "error:", err)
			case op := <-builder.testCmd.Output:
				tRes = op
				tDirty = false
			case op := <-builder.buildCmd.Output:
				bRes = op
				bDirty = false
			}
			display(out, bRes, tRes, tDirty, bDirty)
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
