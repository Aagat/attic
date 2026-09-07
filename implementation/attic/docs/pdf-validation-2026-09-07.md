# Kindle Scribe PDF validation — 2026-09-07

The final vLLM sample was generated through the live acquisition, ChatGPT
subscription approval, and Pandoc/XeLaTeX pipeline, then checked independently.

- Source: https://vllm.ai/blog/2026-08-23-speculative-decoding-amd-gpus
- Job: `ba02c564bffceda6d657d54ede2bc699`
- PDF: 121 pages, 497,082 bytes, 157.5 × 210 mm portrait.
- SHA-256: `7b9184f84b7ff08b1f4de9a5c5ade8875dec9684e8a327d0504969fe479ba182`
- Title and author properties match the article; author is AMD and Embedded LLM.
  Subject contains publisher, publication date and source URL; creator is Attic.

## Post-generation pass

The saved source HTML was inventoried with Python's HTMLParser and compared with
Poppler text extraction. Unicode ligatures, whitespace and page-number breaks
were normalized for comparisons. These are content-presence checks, not a proof
of every aspect of reading order or semantics.

| Check | Result |
| --- | --- |
| Main-article section headings | All 128 present; four related-post headings excluded |
| Source code blocks | All 36 found after normalization |
| Source tables | 90 tables, all 5,229 source cell texts found in PDF text |
| SVG diagrams | All three embedded as PNGs, each 1,200 pixels wide |
| Text outside page bounds | None found with a 10-point edge guard using `pdftotext -bbox` |
| Metadata and hyperlinks | Verified with `pdfinfo` and `pdfinfo -url` |
| Visual inspection | All 121 pages rendered into contact sheets; figures, tables, code and ending also inspected at larger scale |

Wide tables continue as column groups with repeated row labels. This preserves
the full appendix and increases the page count. Interactive heatmaps are static
tables of values; their controls and original color encoding are not reproduced.
CSS-only process diagrams retain their text in a linear layout. The PDF was
previewed remotely, not on physical Kindle hardware.

Regression checks passed: `go test ./...`, `go vet ./...`, and the real
`TestPandocPDFIntegration` with `ATTIC_LATEX_INTEGRATION=1`. The integration fixture
checks Unicode metadata, literal TeX-like code, code inside headings, check/cross
glyphs, embedded images, wide-table cell preservation, clickable links and page
bounds.

The earlier jyn sample was also regenerated with metadata. Its author property
is `jyn`; title, publisher, publication date and original link are populated.

## Economist result

Source: https://www.economist.com/finance-and-economics/2026/09/04/the-jobs-apocalypse-is-postponed-an-ai-jobs-boom-is-here

Job `2da18a3e7537be469593eeff7d5aeaec` returned `access_denied`. The public
request received an access-denied page, so no article PDF was generated. Full
content rendering for this source remains unverified.

## Local artifacts

Downloaded articles and generated PDFs are deliberately untracked:
`data/formatting/vllm-source.html`, `data/formatting/vllm-verification.txt`,
`data/validation/vllm.pdf`, and `data/validation/vllm-job.json` at the repository
root. The LAN preview serves `data/preview/` on port 18081 and offers a selector
for the jyn and vLLM samples.
