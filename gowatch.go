package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"

	"gopkg.in/fatih/color.v0"
	"gopkg.in/fsnotify.v1"

	"github.com/spf13/afero"
)

func main() {
	Main(os.Stdout, os.Stderr, afero.OsFs{}, os.Args)
}

func runTests(out, eout io.Writer) string {
	output := bytes.NewBufferString("")
	ok := color.New(color.Bold, color.FgGreen).SprintFunc()
	bad := color.New(color.Bold, color.FgRed).SprintFunc()

	cmd := exec.Command("go", "build", "./...")
	cout, err := cmd.Output()
	if err != nil {
		fmt.Fprintln(output, bad("Build ERROR:"), err)
	} else {
		fmt.Fprintln(output, ok("Build OK"))
	}

	cmd = exec.Command("go", "test", "-v", "./...")
	cout, err = cmd.Output()
	if err != nil {
		fmt.Fprintln(output, bad(err))
	} else {
		fmt.Fprintln(output, string(cout))
		fmt.Fprintln(output, ok("Tests OK"))
	}

	return output.String()
}

func clear(eout io.Writer) {
	cmd := exec.Command("clear")
	cmd.Stdout = os.Stdout

	err := cmd.Run()
	if err != nil {
		fmt.Fprintln(eout, err)
	}
}

// DoBuild executes a build and test run
func DoBuild(out, eout io.Writer) {
	fmt.Fprintln(out, "...")
	output := runTests(out, eout)
	clear(eout)
	fmt.Fprintln(out, output)
}

// Main function
func Main(out io.Writer, eout io.Writer, fs afero.Fs, args []string) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}

	done := make(chan bool)

	DoBuild(eout, out)
	// Process event
	go func() {
		for {
			select {
			case ev := <-watcher.Events:
				//fmt.Fprintln(out, "event:", ev)
				if !strings.HasSuffix(ev.Name, ".go") {
					continue
				}
				DoBuild(eout, out)
			case err := <-watcher.Errors:
				fmt.Fprintln(eout, "error:", err)
			}
		}
	}()

	err = watcher.Add(".")
	if err != nil {
		log.Fatal(err)
	}

	// Hang so program doesn't exit
	<-done

	/* ... do stuff ... */
	watcher.Close()
	return nil
}
