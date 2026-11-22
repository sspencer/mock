package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
)

const completionUsage = "usage: mock completion bash|zsh|fish|powershell"

// completionShells are the shells mock completion writes a script for.
var completionShells = []string{"bash", "zsh", "fish", "powershell"}

// runCompletion writes a shell completion script. The flag and subcommand
// lists come from usageText, so they cannot drift from mock -h.
func runCompletion(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("mock completion", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(stdout, completionUsage)
			return nil
		}
		return usageError("%v\n%s", err, completionUsage)
	}
	if flags.NArg() != 1 {
		return usageError("name one shell\n%s", completionUsage)
	}
	model := commandLineModel()
	var script string
	switch flags.Arg(0) {
	case "bash":
		script = bashCompletion(model)
	case "zsh":
		script = zshCompletion(model)
	case "fish":
		script = fishCompletion(model)
	case "powershell", "pwsh":
		script = powershellCompletion(model)
	default:
		return usageError("unknown shell %q\n%s", flags.Arg(0), completionUsage)
	}
	_, err := io.WriteString(stdout, script)
	return err
}

// flagDoc is one flag in usageText's flags section.
type flagDoc struct {
	short, long string
	// value is the value placeholder, such as PORT; "" for a boolean flag.
	value string
	// spec is the flag as usageText writes it, such as "-p, --port PORT".
	spec string
	// lines is the description; a line indented past the description column
	// (a list under the flag) keeps its extra indent.
	lines []string
}

// names returns the flag's names with their dashes: -p and --port.
func (f flagDoc) names() []string {
	var names []string
	if f.short != "" {
		names = append(names, "-"+f.short)
	}
	return append(names, "--"+f.long)
}

// subcommand is a mock subcommand and its flags (with one dash, as its usage
// line writes them). run takes mock's own flags.
type subcommand struct {
	name, usage string
	flags       []string
}

// commandLine is what usageText says about the command line.
type commandLine struct {
	synopsis    []string
	flags       []flagDoc
	subcommands []subcommand
}

func (c commandLine) subcommandNames() []string {
	var names []string
	for _, sub := range c.subcommands {
		names = append(names, sub.name)
	}
	return names
}

// mainFlagNames lists every name of mock's own flags.
func (c commandLine) mainFlagNames() []string {
	var names []string
	for _, f := range c.flags {
		names = append(names, f.names()...)
	}
	return names
}

// valueFlagNames lists the names of mock's own flags that take a value.
func (c commandLine) valueFlagNames() []string {
	var names []string
	for _, f := range c.flags {
		if f.value != "" {
			names = append(names, f.names()...)
		}
	}
	return names
}

var (
	// flagSpecPattern reads a flag line: short and long name, an upper-case
	// or quoted value placeholder, and the start of the description.
	flagSpecPattern   = regexp.MustCompile(`^((?:-([A-Za-z]), )?--([a-z0-9-]+)(?: ('[^']*'|[A-Z][A-Z0-9_=]*))?)(?:\s+(.*))?$`)
	usageFlagPattern  = regexp.MustCompile(`(?:^|[\s\[|])-([A-Za-z][A-Za-z-]*)`)
	subcommandPattern = regexp.MustCompile(`^mock ([a-z]+)(?: |$)`)
	descriptionColumn = 23
	maxFlagSpecIndent = 6
)

// commandLineModel parses usageText. TestCommandLineModel checks that every
// flag parseConfig defines is found.
func commandLineModel() commandLine {
	var model commandLine
	usage, flagsText, _ := strings.Cut(usageText, "\nflags:\n")
	for _, line := range strings.Split(usage, "\n")[1:] {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		model.synopsis = append(model.synopsis, line)
		match := subcommandPattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		if slices.Contains(model.subcommandNames(), match[1]) {
			continue
		}
		sub := subcommand{name: match[1], usage: line}
		if sub.name != "run" && sub.name != "serve" {
			for _, flagMatch := range usageFlagPattern.FindAllStringSubmatch(line[len(match[0]):], -1) {
				if name := "-" + flagMatch[1]; !slices.Contains(sub.flags, name) {
					sub.flags = append(sub.flags, name)
				}
			}
		}
		model.subcommands = append(model.subcommands, sub)
	}
	for _, line := range strings.Split(flagsText, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		text := strings.TrimSpace(line)
		if indent <= maxFlagSpecIndent && strings.HasPrefix(text, "-") {
			match := flagSpecPattern.FindStringSubmatch(text)
			if match == nil {
				continue
			}
			doc := flagDoc{spec: match[1], short: match[2], long: match[3], value: match[4]}
			if description := strings.TrimSpace(match[5]); description != "" {
				doc.lines = append(doc.lines, description)
			}
			model.flags = append(model.flags, doc)
			continue
		}
		if len(model.flags) == 0 {
			continue
		}
		last := &model.flags[len(model.flags)-1]
		if extra := indent - descriptionColumn; extra > 0 {
			text = strings.Repeat(" ", extra) + text
		}
		last.lines = append(last.lines, text)
	}
	return model
}

// topFlagNames are the flags mock accepts without a subcommand; server flags
// need mock serve or mock run.
var topFlagNames = []string{"-h", "-help", "-version"}

func bashCompletion(model commandLine) string {
	var subs strings.Builder
	for _, sub := range model.subcommands {
		if sub.name == "run" || sub.name == "serve" || sub.name == "completion" || sub.name == "help" {
			continue
		}
		fmt.Fprintf(&subs, "        %s) flags=%q ;;\n", sub.name, strings.Join(sub.flags, " "))
	}
	return fmt.Sprintf(`# bash completion for mock. Load it with:
#   eval "$(mock completion bash)"
_mock() {
    local cur="${COMP_WORDS[COMP_CWORD]}" prev="${COMP_WORDS[COMP_CWORD-1]}"
    local cmd="" flags=""
    if [[ $COMP_CWORD -gt 1 ]]; then
        case "${COMP_WORDS[1]}" in
            %[1]s) cmd="${COMP_WORDS[1]}" ;;
        esac
    fi
    case "$cmd" in
        completion)
            COMPREPLY=($(compgen -W %[2]q -- "$cur"))
            return ;;
        help)
            COMPREPLY=($(compgen -W %[6]q -- "$cur"))
            return ;;
        "")
            if [[ "$cur" == -* ]]; then
                COMPREPLY=($(compgen -W %[7]q -- "$cur"))
            elif [[ $COMP_CWORD -eq 1 ]]; then
                COMPREPLY=($(compgen -W %[6]q -- "$cur"))
            fi
            return ;;
%[3]s        *)
            case "$prev" in
                %[4]s)
                    compopt -o default 2>/dev/null
                    COMPREPLY=()
                    return ;;
            esac
            flags=%[5]q ;;
    esac
    if [[ "$cur" == -* ]]; then
        COMPREPLY=($(compgen -W "$flags" -- "$cur"))
        return
    fi
    compopt -o filenames 2>/dev/null
    COMPREPLY=($(compgen -d -- "$cur") $(compgen -f -X '!*.http' -- "$cur"))
}
complete -F _mock mock
`, strings.Join(model.subcommandNames(), "|"), strings.Join(completionShells, " "), subs.String(),
		strings.Join(model.valueFlagNames(), "|"), strings.Join(model.mainFlagNames(), " "),
		strings.Join(model.subcommandNames(), " "), strings.Join(topFlagNames, " "))
}

func zshCompletion(model commandLine) string {
	var subs strings.Builder
	for _, sub := range model.subcommands {
		if sub.name == "run" || sub.name == "serve" || sub.name == "completion" || sub.name == "help" {
			continue
		}
		fmt.Fprintf(&subs, "    %s) flags=(%s) ;;\n", sub.name, strings.Join(sub.flags, " "))
	}
	return fmt.Sprintf(`#compdef mock
# zsh completion for mock. Save it as _mock in a directory on $fpath, or
# load it with: source <(mock completion zsh)
_mock() {
  local -a subcommands flags
  subcommands=(%[1]s)
  local cmd=""
  if (( CURRENT > 2 )) && (( ${subcommands[(Ie)${words[2]}]} )); then
    cmd=${words[2]}
  fi
  case $cmd in
    completion)
      compadd -- %[2]s
      return ;;
    help)
      compadd -- $subcommands
      return ;;
    '')
      if [[ $PREFIX == -* ]]; then
        compadd -- %[6]s
      elif (( CURRENT == 2 )); then
        compadd -- $subcommands
      fi
      return ;;
%[3]s    *)
      case ${words[CURRENT-1]} in
        %[4]s)
          _files
          return ;;
      esac
      flags=(%[5]s) ;;
  esac
  if [[ $PREFIX == -* ]]; then
    compadd -- $flags
    return
  fi
  _files -g '*.http(-.)'
}
if [[ $funcstack[1] == _mock ]]; then
  _mock "$@"
else
  compdef _mock mock
fi
`, strings.Join(model.subcommandNames(), " "), strings.Join(completionShells, " "), subs.String(),
		strings.Join(model.valueFlagNames(), "|"), strings.Join(model.mainFlagNames(), " "),
		strings.Join(topFlagNames, " "))
}

func fishCompletion(model commandLine) string {
	var out strings.Builder
	out.WriteString("# fish completion for mock. Save it as ~/.config/fish/completions/mock.fish,\n# or load it with: mock completion fish | source\n")
	fmt.Fprintf(&out, "complete -c mock -n __fish_use_subcommand -f -a %q\n", strings.Join(model.subcommandNames(), " "))
	fmt.Fprintf(&out, "complete -c mock -n '__fish_seen_subcommand_from completion' -f -a %q\n", strings.Join(completionShells, " "))
	fmt.Fprintf(&out, "complete -c mock -n '__fish_seen_subcommand_from help' -f -a %q\n", strings.Join(model.subcommandNames(), " "))
	for _, name := range topFlagNames {
		fmt.Fprintf(&out, "complete -c mock -n __fish_use_subcommand -o %s\n", strings.TrimPrefix(name, "-"))
	}
	own := "'__fish_seen_subcommand_from serve run'"
	for _, f := range model.flags {
		line := "complete -c mock -n " + own
		if f.short != "" {
			line += " -s " + f.short
		}
		line += " -l " + f.long
		if f.value != "" {
			line += " -r"
		}
		if len(f.lines) > 0 {
			line += " -d " + fishQuote(strings.TrimSpace(f.lines[0]))
		}
		out.WriteString(line + "\n")
	}
	for _, sub := range model.subcommands {
		for _, name := range sub.flags {
			fmt.Fprintf(&out, "complete -c mock -n '__fish_seen_subcommand_from %s' -o %s\n", sub.name, strings.TrimPrefix(name, "-"))
		}
	}
	return out.String()
}

func fishQuote(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'"
}

func powershellCompletion(model commandLine) string {
	quote := func(items []string) string {
		quoted := make([]string, len(items))
		for i, item := range items {
			quoted[i] = "'" + strings.ReplaceAll(item, "'", "''") + "'"
		}
		return "@(" + strings.Join(quoted, ", ") + ")"
	}
	var flags strings.Builder
	fmt.Fprintf(&flags, "        'mock' = %s\n", quote(topFlagNames))
	fmt.Fprintf(&flags, "        'serve' = %s\n", quote(model.mainFlagNames()))
	for _, sub := range model.subcommands {
		if sub.name != "run" && sub.name != "serve" {
			fmt.Fprintf(&flags, "        '%s' = %s\n", sub.name, quote(sub.flags))
		}
	}
	return fmt.Sprintf(`# PowerShell completion for mock. Load it with:
#   mock completion powershell | Out-String | Invoke-Expression
Register-ArgumentCompleter -Native -CommandName mock -ScriptBlock {
    param($wordToComplete, $commandAst, $cursorPosition)
    $words = @($commandAst.CommandElements | ForEach-Object { $_.ToString() })
    $subcommands = %[1]s
    $flags = @{
%[2]s    }
    $cmd = 'mock'
    if ($words.Count -gt 1 -and $subcommands -contains $words[1] -and ($words.Count -gt 2 -or $wordToComplete -eq '')) {
        $cmd = $words[1]
    }
    if ($cmd -eq 'run') { $cmd = 'serve' }
    if ($cmd -eq 'completion') {
        $candidates = %[3]s
    } elseif ($cmd -eq 'help') {
        $candidates = $subcommands
    } elseif ($wordToComplete -like '-*') {
        $candidates = $flags[$cmd]
    } elseif ($cmd -eq 'mock' -and $words.Count -le 2) {
        $candidates = $subcommands
    } else {
        return
    }
    $candidates | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
        [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
    }
}
`, quote(model.subcommandNames()), flags.String(), quote(completionShells))
}
