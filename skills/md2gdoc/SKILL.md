---
name: md2gdoc
description: Convert Markdown into Google Docs API requests or importable HTML with md2gdoc. Use when inserting formatted Markdown into an existing Google Doc or creating a Google Doc from a Markdown file.
---

# Markdown to Google Docs

Use `md2gdoc` for conversion and the user's available Google Docs/Drive client for
API calls. The CLI makes no API calls and handles no authentication. Converting
a file alone does not authorize uploading it or changing a Google Doc.

Check `md2gdoc --help` for installed options. If missing, install a binary from
[Releases](https://github.com/javiermolinar/md2gdoc/releases), or use
`go install github.com/javiermolinar/md2gdoc@latest` with Go 1.22+.

## Choose the output

- **JSON requests:** insert formatted text, lists, and native tables into an
  existing document. Tables are supported in a single batch.
- **HTML:** preview locally or import a complete new document through Drive;
  supports images and more Markdown constructs. Do not switch an existing-doc
  insertion task to creating a new document without the user's direction.

```bash
md2gdoc input.md > requests.json
md2gdoc input.md --format html > document.html
```

Leading YAML frontmatter is preserved as literal metadata. When local or
fragment-only links have no web destination, use `--relative-links text` to keep
both their label and path visible. Do not invent public URLs for local files.

Pipe Markdown into `md2gdoc`, or use `-` for explicit stdin. A bare interactive
invocation shows help. Check conversion succeeds before using
the output: shell redirection can leave an empty file after an error. Preserve
the source content when handling unsupported constructs; do not silently drop
them to make conversion succeed.

## Insert into an existing document

1. Read the current document with tab content (`documents.get` with
   `includeTabsContent: true`). Resolve the intended tab and insertion point
   from its native structure. Indices are UTF-16 code units, not byte or Unicode
   character counts, and insertion must be at a paragraph boundary.
2. Generate requests using the resolved index and tab:

   ```bash
   md2gdoc input.md --start-index "$START_INDEX" --tab-id "$TAB_ID" > requests.json
   ```

   Defaults are index 1 in the first tab; these are not append defaults.
   The converter inserts content only. Replacing content requires a separately
   planned deletion of the requested range and indices valid after that deletion.
3. Keep the generated requests in their original order in the complete
   `{ "requests": [...] }` body. Cell insertion and list formatting depend on
   that order. Use `--requests-only` only when the API client expects the array
   rather than the body.
4. For placement based on a document read, add
   `writeControl.requiredRevisionId` to the body using that read's revision ID.
   Submit the body to `documents.batchUpdate`. If the revision is stale, read
   again and regenerate. After an uncertain API outcome, inspect the document
   before retrying: insertion is not idempotent.
5. Read back the target tab and check text, native tables, formatting, and the
   surrounding content. Report the document link and any verification limits.

For an authenticated [gws](https://github.com/googleworkspace/cli) installation,
after preparing the body and replacing `DOCUMENT_ID`:

```bash
gws docs documents batchUpdate \
  --params '{"documentId":"DOCUMENT_ID"}' \
  --json "$(cat requests.json)"
```

For bodies too large for command-line argument limits, use an API client that
accepts a file or stream. Do not split the ordered batch arbitrarily.

## Import HTML as a new document

Generate `document.html`, then use Drive `files.create` to upload it as
`text/html` with destination MIME type `application/vnd.google-apps.document`.
Use the requested title and folder. With `gws` v0.22.5 or another version exposing
`--upload-content-type` in `gws drive files create --help`, run from the directory
containing `document.html`:

```bash
gws drive files create \
  --json '{"name":"Document title","mimeType":"application/vnd.google-apps.document"}' \
  --upload document.html --upload-content-type text/html
```

Older versions such as v0.3.5 use the destination MIME type for the media part,
which is unsuitable for conversion. Upgrade or use a Drive client that accepts
separate source and destination MIME types; a dry run alone cannot verify this.

Read back the created document to check its content and tables; visually inspect
when layout matters. Local HTML preview alone does not verify Google's import.
Image URLs must be absolute HTTPS URLs accessible to the importer.
JSON requests keep table rows and code blocks together where possible and repeat
table headers. HTML print hints may not survive Drive import. Blocks taller than
a page still need to split; inspect the resulting document when layout matters.
