# Vendored PDF.js

Source: https://github.com/mozilla/pdf.js
Distribution: https://registry.npmjs.org/pdfjs-dist/-/pdfjs-dist-6.3.289.tgz
Version: 6.3.289
License: Apache-2.0; see LICENSE and per-directory font/decoder notices.

Tarball integrity (SHA-512, verified before extraction):
`ZHjSVpDa3D6izMq8/04lvkhkATUmL9px6ChPaXc1k6nU2Mrhlg1/7F0bdUqCwUjw3NsPTfPZsMDUU6ZIcRaeQw==`

Copied legacy/build/pdf.mjs and pdf.worker.mjs, standard_fonts/, cmaps/, wasm/,
and LICENSE. The legacy build includes browser compatibility polyfills.
Dependencies are served locally; no document bytes are sent to a CDN.

When updating, verify the registry integrity, replace these assets together,
retain upstream licenses, and run the real PDF preview browser integration test.
