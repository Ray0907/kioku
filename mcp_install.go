package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mattn/go-isatty"
)

var mcpInstallAgents = []struct {
	name        string
	add, remove []string
}{
	{"claude", []string{"mcp", "add", "--scope", "user", "kioku", "--"}, []string{"mcp", "remove", "--scope", "user", "kioku"}},
	{"codex", []string{"mcp", "add", "kioku", "--"}, nil},
	{"pi", []string{"mcp", "add", "kioku", "--"}, nil},
	// shortcut: Grok is unavailable to verify replacement, remove first until its CLI is verified.
	{"grok", []string{"mcp", "add", "kioku", "--"}, []string{"mcp", "remove", "kioku"}},
}

func mcpInstallExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if strings.Contains(filepath.ToSlash(exe), "/go-build") && filepath.Base(filepath.Dir(exe)) == "exe" {
		return "", fmt.Errorf("cannot register a go run temporary executable; install kioku first")
	}
	path, lookupErr := exec.LookPath("kioku")
	if lookupErr == nil || errors.Is(lookupErr, exec.ErrDot) {
		running, err := os.Stat(exe)
		entry, statErr := os.Stat(path)
		if err == nil && statErr == nil && os.SameFile(running, entry) {
			return filepath.Abs(path)
		}
	}
	return exe, nil
}

// mcpInstallAgentPath finds an agent on PATH. A match that comes from the current directory or a
// relative PATH entry is refused: a repository could plant an executable with an agent's name.
func mcpInstallAgentPath(name string) (string, error) {
	bin, err := exec.LookPath(name)
	if err == nil && !filepath.IsAbs(bin) {
		err = exec.ErrDot
	}
	return bin, err
}

func runMCPInstall(args []string) error {
	yes, dry := false, false
	var selected []int
	known := make([]string, len(mcpInstallAgents))
	for i, agent := range mcpInstallAgents {
		known[i] = agent.name
	}
	for _, arg := range args {
		switch arg {
		case "--yes":
			yes = true
		case "--dry-run":
			dry = true
		default:
			index := -1
			for i, agent := range mcpInstallAgents {
				if arg == agent.name {
					index = i
					break
				}
			}
			if index < 0 {
				return fmt.Errorf("unknown agent %q; known agents: %s; usage: kioku mcp install [agent...] [--yes] [--dry-run]", arg, strings.Join(known, ", "))
			}
			if !slices.Contains(selected, index) {
				selected = append(selected, index)
			}
		}
	}
	if len(selected) == 0 {
		for i, agent := range mcpInstallAgents {
			if _, err := mcpInstallAgentPath(agent.name); err == nil {
				selected = append(selected, i)
			}
		}
	}
	if len(selected) == 0 {
		return fmt.Errorf("no known agents found on PATH (known agents: %s)", strings.Join(known, ", "))
	}
	exe, err := mcpInstallExecutable()
	if err != nil {
		return err
	}
	names := make([]string, len(selected))
	for i, index := range selected {
		names[i] = mcpInstallAgents[index].name
	}
	fmt.Fprintf(output, "Agents: %s\n", strings.Join(names, ", "))
	for _, index := range selected {
		agent := mcpInstallAgents[index]
		commands := [][]string{}
		if agent.remove != nil {
			commands = append(commands, agent.remove)
		}
		commands = append(commands, append(append([]string{}, agent.add...), exe, "mcp"))
		for _, argv := range commands {
			printed := append([]string{agent.name}, argv...)
			for i, arg := range printed {
				if strings.ContainsAny(arg, " \t\n\r\"'\\$`;&|<>()*?[]{}!#~") {
					printed[i] = shellQuote(arg)
				}
			}
			fmt.Fprintln(output, strings.Join(printed, " "))
		}
	}
	if dry {
		return nil
	}
	if !yes {
		if !isatty.IsTerminal(os.Stdin.Fd()) {
			return fmt.Errorf("stdin is not a terminal; use --yes to register these agents")
		}
		fmt.Fprint(output, "Register kioku with these agents? [y/N] ")
		answer, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && err != io.EOF {
			return err
		}
		answer = strings.TrimSpace(answer)
		if !strings.EqualFold(answer, "y") && !strings.EqualFold(answer, "yes") {
			return fmt.Errorf("registration cancelled")
		}
	}
	failed := false
	for _, index := range selected {
		agent := mcpInstallAgents[index]
		bin, err := mcpInstallAgentPath(agent.name)
		if err == nil && agent.remove != nil {
			var stdout, stderr bytes.Buffer
			cmd := exec.Command(bin, agent.remove...)
			cmd.Stdout, cmd.Stderr = io.MultiWriter(output, &stdout), io.MultiWriter(os.Stderr, &stderr)
			err = cmd.Run()
			if err != nil {
				message := strings.ToLower(stdout.String() + stderr.String())
				if strings.Contains(message, "no mcp server named \"kioku\"") || strings.Contains(message, "not found") {
					err = nil
				}
			}
		}
		if err == nil {
			argv := append(append([]string{}, agent.add...), exe, "mcp")
			cmd := exec.Command(bin, argv...)
			cmd.Stdout, cmd.Stderr = output, os.Stderr
			err = cmd.Run()
		}
		if err != nil {
			fmt.Fprintf(output, "%s: FAILED: %v\n", agent.name, err)
			failed = true
		} else {
			fmt.Fprintf(output, "%s: OK\n", agent.name)
		}
	}
	if failed {
		return fmt.Errorf("MCP registration failed for one or more agents")
	}
	fmt.Fprintln(output, "restart the agents to load the server.")
	return nil
}
