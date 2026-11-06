package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/term"
)

// interactive reports whether a person is at the terminal: stdin and stdout
// both TTYs.
func interactive() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

var stdin = bufio.NewReader(os.Stdin)

func readLine() (string, bool) {
	s, err := stdin.ReadString('\n')
	if err != nil && s == "" {
		return "", false
	}
	return strings.TrimSpace(s), true
}

// confirmYes asks a question that defaults to yes.
func confirmYes(prompt string) bool {
	fmt.Printf("%s [Y/n] ", prompt)
	s, ok := readLine()
	if !ok {
		return false
	}
	s = strings.ToLower(s)
	return s == "" || s == "y" || s == "yes"
}

// choose shows numbered options and returns the index picked (0 is the
// default on a bare enter), or -1 when input ends.
func choose(prompt string, options []string) int {
	fmt.Println(prompt)
	for i, o := range options {
		marker := " "
		if i == 0 {
			marker = "▸"
		}
		fmt.Printf("  %s %d) %s\n", marker, i+1, o)
	}
	for {
		fmt.Printf("Choose 1-%d [1]: ", len(options))
		s, ok := readLine()
		if !ok {
			return -1
		}
		if s == "" {
			return 0
		}
		if n, err := strconv.Atoi(s); err == nil && n >= 1 && n <= len(options) {
			return n - 1
		}
	}
}

// ask reads a line with a default.
func ask(prompt, def string) string {
	if def != "" {
		fmt.Printf("%s [%s]: ", prompt, def)
	} else {
		fmt.Printf("%s: ", prompt)
	}
	s, ok := readLine()
	if !ok || s == "" {
		return def
	}
	return s
}

// openBrowser opens url in the default browser, best effort.
func openBrowser(url string) bool {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		return false
	}
	return cmd.Start() == nil
}
