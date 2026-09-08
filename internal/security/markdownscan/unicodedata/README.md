# Vendored Unicode data

`emoji-variation-sequences.txt` is the Unicode Consortium's list of every
valid `<base> <variation selector>` pair, published alongside
[UTS #51](https://www.unicode.org/reports/tr51). The scanner embeds it with
`go:embed` and uses it to tell an emoji presentation sequence such as `⚠️`
(`U+26A0 U+FE0F`) apart from a variation selector attached to a character
that has no such sequence.

The copy is pinned to one Unicode version, recorded in the file's own
`# Version:` header and in `UnicodeVersion` in `emoji.go`.
`emoji_test.go` fails the build if those two disagree, or if the number of
sequences parsed does not match the `#Total sequences:` trailer the
Consortium writes at the end of the file.

## Why it is vendored

A security scanner should not have a network dependency in its hot path, and
a pinned copy keeps scans reproducible: the same input produces the same
findings on every machine and every run. Tracking `latest/` would let an
upstream data change silently shift what blocks an install.

## Refreshing

The file is published per Unicode version; do not fetch `latest/`.

```bash
UNICODE_VERSION=17.0.0
curl -fsSL "https://www.unicode.org/Public/${UNICODE_VERSION}/ucd/emoji/emoji-variation-sequences.txt" \
  -o internal/security/markdownscan/unicodedata/emoji-variation-sequences.txt
```

Then bump `UnicodeVersion` in `emoji.go` to match the new file's
`# Version:` header and run `go test ./internal/security/markdownscan/`.

The data is © Unicode, Inc. and redistributed under the
[Unicode License](https://www.unicode.org/license.txt).
