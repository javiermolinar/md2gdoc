package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
)

var version = "dev" // Set by GoReleaser; go install gets its module version below.

const help = `Usage: md2gdoc [FILE|-] [options]

Convert Markdown to Google Docs requests or a standalone HTML document.
Reads stdin when FILE is omitted or '-'. Makes no API calls.

Options:
  --format FORMAT   Output format: requests (default) or html
  --start-index N    UTF-16 insertion index at a paragraph boundary (default: 1)
  --tab-id ID        Target document tab (default: first tab)
  --relative-links MODE  Local links: error (default) or text (label and path)
  --requests-only   Output the requests array instead of { "requests": [...] }
  --compact         Emit compact JSON instead of pretty-printed JSON
  -h, --help        Show this help
  --version         Show the version

Examples:
  md2gdoc section.md --start-index 6927 --tab-id t.0 > requests.json
  md2gdoc document.md --format html > document.html
  printf '# Summary\n\nHello **world**.\n' | md2gdoc

The caller handles authentication, API calls, deletion of replaced content,
revision guards, and finding the insertion point. HTML uploads are also external.
--start-index, --tab-id, --requests-only, and --compact require requests format.
`

type cliOptions struct {
	format, file                         string
	convert                              options
	requestsOnly, compact, help, version bool
}

// Accept options on either side of the filename, without a CLI dependency.
func parseArgs(args []string) (cliOptions, error) {
	o := cliOptions{format: "requests"}
	var files []string
	positional := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if positional || !strings.HasPrefix(arg, "-") || arg == "-" {
			files = append(files, arg)
			continue
		}
		if arg == "--" {
			positional = true
			continue
		}
		name, value, hasValue := strings.Cut(arg, "=")
		switch name {
		case "--format", "--start-index", "--tab-id", "--relative-links":
			if !hasValue {
				if i+1 == len(args) || strings.HasPrefix(args[i+1], "--") {
					return o, fmt.Errorf("%s requires a value", name)
				}
				i++
				value = args[i]
			}
			switch name {
			case "--format":
				o.format = value
			case "--relative-links":
				if value != "error" && value != "text" {
					return o, fmt.Errorf("--relative-links must be 'error' or 'text'")
				}
				o.convert.RelativeLinks = value
			case "--tab-id":
				o.convert.TabID = &value
			case "--start-index":
				index, err := strconv.ParseInt(value, 10, 64)
				if err != nil || index < 1 || index > maxIndex || value[0] < '1' || value[0] > '9' {
					return o, fmt.Errorf("--start-index must be an integer between 1 and 2147483647")
				}
				o.convert.StartIndex = &index
			}
		case "--requests-only", "--compact", "--help", "-h", "--version":
			if hasValue {
				return o, fmt.Errorf("%s does not take a value", name)
			}
			switch name {
			case "--requests-only":
				o.requestsOnly = true
			case "--compact":
				o.compact = true
			case "--help", "-h":
				o.help = true
			case "--version":
				o.version = true
			}
		default:
			return o, fmt.Errorf("unknown option %s", name)
		}
	}
	if o.help || o.version {
		return o, nil
	}
	if len(files) > 1 {
		return o, fmt.Errorf("expected at most one input file")
	}
	if len(files) == 1 {
		o.file = files[0]
	}
	if o.format != "requests" && o.format != "html" {
		return o, fmt.Errorf("--format must be 'requests' or 'html'")
	}
	if o.format == "html" && (o.convert.StartIndex != nil || o.convert.TabID != nil || o.requestsOnly || o.compact) {
		return o, fmt.Errorf("--start-index, --tab-id, --requests-only, and --compact require --format requests")
	}
	return o, nil
}

func buildVersion() string {
	if version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return strings.TrimPrefix(info.Main.Version, "v")
	}
	return version
}

func execute(args []string, stdin io.Reader, stdout io.Writer) error {
	o, err := parseArgs(args)
	if err != nil {
		return err
	}
	if o.help {
		_, err = io.WriteString(stdout, help)
		return err
	}
	if o.version {
		_, err = fmt.Fprintln(stdout, buildVersion())
		return err
	}
	var input []byte
	if o.file == "" || o.file == "-" {
		input, err = io.ReadAll(stdin)
	} else {
		input, err = os.ReadFile(o.file)
	}
	if err != nil {
		return err
	}
	var output bytes.Buffer
	if o.format == "html" {
		html, err := renderHTMLWithOptions(string(input), o.convert)
		if err != nil {
			return err
		}
		output.WriteString(html)
	} else {
		body, err := convert(string(input), o.convert)
		if err != nil {
			return err
		}
		var result any = body
		if o.requestsOnly {
			result = body.Requests
		}
		encoder := json.NewEncoder(&output)
		encoder.SetEscapeHTML(false)
		if !o.compact {
			encoder.SetIndent("", "  ")
		}
		if err := encoder.Encode(result); err != nil {
			return err
		}
	}
	// Conversion must succeed before stdout receives anything.
	_, err = output.WriteTo(stdout)
	return err
}

func main() {
	if err := execute(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "md2gdoc: %v\n", err)
		os.Exit(1)
	}
}
