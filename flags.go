package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

type commandFlags struct {
	fs                                                                *flag.FlagSet
	rebuild, jsonFlag, here, allTime, includeSelf, includeAutomated   *bool
	all, full, outline, sessions, facets, noRedact, noMouse, ver      *bool
	limit, contextSize, maxPerSession                                 *int
	cursor, showQuery, role, modelFlag, format, since, until, harness *string
	projects                                                          projectFlags
}

func newFlagSet() *commandFlags {
	fs := flag.NewFlagSet("kioku", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	opts := &commandFlags{fs: fs}
	opts.rebuild = fs.Bool("rebuild", false, "rebuild index")
	opts.jsonFlag = fs.Bool("json", false, "print JSON lines")
	opts.limit = fs.Int("limit", 0, "maximum results")
	opts.cursor = fs.String("cursor", "", "next page token")
	opts.showQuery = fs.String("query", "", "locate the hit in show")
	opts.contextSize = fs.Int("context", 3, "messages before and after")
	opts.here = fs.Bool("here", false, "scope to the current project directory")
	opts.allTime = fs.Bool("all-time", false, "include older short-prefix matches")
	opts.includeSelf = fs.Bool("include-self", false, "include kioku tool calls and results")
	opts.includeAutomated = fs.Bool("include-automated", false, "include automated review sessions")
	opts.role = fs.String("role", "", "filter roles: user, asst or tool (comma-separated)")
	opts.modelFlag = fs.String("model", "", "filter model by case-insensitive substring")
	opts.all = fs.Bool("all", false, "show full session")
	opts.full = fs.Bool("full", false, "show selected message without truncation")
	opts.outline = fs.Bool("outline", false, "show user messages and each turn's last reply, no tools")
	opts.sessions = fs.Bool("sessions", false, "group by session")
	opts.format = fs.String("format", "text", "show output: text or md")
	opts.since = fs.String("since", "", "only messages at or after this date")
	opts.until = fs.String("until", "", "only messages before the end of this date")
	opts.facets = fs.Bool("facets", false, "add hit counts per project, harness and week")
	opts.maxPerSession = fs.Int("max-per-session", 0, "keep at most N hits per session")
	opts.noRedact = fs.Bool("no-redact", false, "do not redact credentials in output")
	opts.noMouse = fs.Bool("no-mouse", false, "disable mouse")
	opts.harness = fs.String("harness", "all", harnessFlagHelp())
	fs.Var(&opts.projects, "project", "filter project basename (repeatable)")
	fs.Var(&opts.projects, "p", "filter project basename (repeatable)")
	opts.ver = fs.Bool("version", false, "print version")
	return opts
}

// Flags may precede or follow the query; single-dash names other than -p are query words.
func parseArgs(fs *flag.FlagSet, args []string) (words []string, help bool, err error) {
	flags := map[string]bool{}
	fs.VisitAll(func(f *flag.Flag) {
		name := "--" + f.Name
		if f.Name == "p" {
			name = "-p"
		}
		boolean, ok := f.Value.(interface{ IsBoolFlag() bool })
		flags[name] = !ok || !boolean.IsBoolFlag()
	})
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			words = append(words, args[i+1:]...)
			break
		}
		if a == "--help" || a == "-h" {
			return words, true, nil
		}
		if takesValue, known := flags[a]; known {
			if takesValue {
				if i+1 == len(args) {
					// A bare trailing --harness has always been a query word.
					if a == "--harness" {
						words = append(words, a)
						continue
					}
					return nil, false, fmt.Errorf("%s requires a value", a)
				}
				if err := fs.Parse(args[i : i+2]); err != nil {
					return nil, false, err
				}
				i++
			} else if err := fs.Parse([]string{a}); err != nil {
				return nil, false, err
			}
			continue
		}
		if name, _, ok := strings.Cut(a, "="); ok && flags[name] {
			if err := fs.Parse([]string{a}); err != nil {
				return nil, false, err
			}
			continue
		}
		words = append(words, a)
	}
	return words, false, nil
}
