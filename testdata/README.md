`requests.json` records inputs and expected request bodies from the TypeScript
implementation before its Go port. It covers paragraphs, inline styles, lists,
tables, UTF-16 indices, tab targeting, and explicit failures. Tests ignore JSON
object-key and field-mask order, plus the newer pagination instructions tested
separately; content, indices, other formatting, and request order must match.

The table request sequence was also verified against Google Docs before the port.
The fixture contains synthetic content only and requires no account or network.
