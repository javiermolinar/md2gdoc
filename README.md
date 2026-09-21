# md2gdoc

Convert Markdown into Google Docs `documents.batchUpdate` requests or standalone HTML.
Use its output with [gws](https://github.com/googleworkspace/cli), an API client, or another tool.
The converter makes no API calls.

## Install

Download the archive for your OS and architecture from
[Releases](https://github.com/javiermolinar/md2gdoc/releases), extract it, and put
`md2gdoc` (`md2gdoc.exe` on Windows) on your `PATH`. No runtime is required.
Each release includes SHA-256 checksums.

Or install with Go 1.22+:

```bash
go install github.com/javiermolinar/md2gdoc@latest
```

Ensure Go's bin directory (`go env GOBIN`, or `$(go env GOPATH)/bin` by default)
is on your `PATH`. Installation requires a published repository revision; binary
downloads require a release.

## Agent skill

An optional [skill](skills/md2gdoc/SKILL.md) is included in release archives.
Copy `skills/md2gdoc` into your agent's skills directory, or ask its skill installer
to install that path from `javiermolinar/md2gdoc`. `go install` installs only the CLI.

## Usage

```bash
# JSON requests (default)
md2gdoc input.md > requests.json

# Insert at a specific paragraph boundary and tab
md2gdoc input.md --start-index 6927 --tab-id t.0 > requests.json

# Standalone HTML
md2gdoc input.md --format html > document.html
```

Pipe Markdown into `md2gdoc`, or use `-` to read stdin explicitly. With no input
at an interactive prompt, it shows help. Run `md2gdoc --help` for options.
Leading YAML frontmatter is preserved as literal metadata. Use `--relative-links text`
to keep local links as their label and path instead of rejecting them.

## Outputs

- **JSON:** a `{ "requests": [...] }` body for one Google Docs `documents.batchUpdate`
  call. Inserts formatted text and native tables into an existing document at a
  paragraph boundary. Defaults to index 1 in the first tab; does not delete content.
- **HTML:** a complete document for browser preview or Google Drive import as a new
  Google Doc. Supports tables, images and more Markdown constructs. Upload separately.

JSON keeps table rows and code blocks together where they fit and repeats table
headers across pages. HTML includes print pagination hints; Drive import may vary.

## Explicit limitations of request output

These constructs fail with an error rather than being silently dropped:

- Images, and tables inside lists or blockquotes.
- Raw HTML, including HTML comments.
- Relative links and fragment-only links (unless `--relative-links text` is set).
- Thematic breaks and explicit hard line breaks.
- Ordered lists starting at a number other than 1.
- Mixed ordered/unordered nesting.
- Code blocks inside lists, and lists combined with blockquotes.
- Control characters and BMP private-use characters that Google Docs strips.

Task-list markers such as `[ ]` remain literal text, not interactive checkboxes.
Footnotes and other nonstandard Markdown extensions are not interpreted.

For creating a **whole new document**, Google Drive's native `text/markdown`
import may be simpler. This converter is useful when a caller needs explicit
Docs API requests to insert formatted Markdown into an existing document.
