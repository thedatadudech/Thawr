# Logo assets

The mark is a cave arch built from a mesh of nodes sheltering a smaller
mesh: the network that is hidden, protected, and reachable only by those
who belong. All files are flat SVG on a transparent background unless
noted.

| File | Use |
|---|---|
| `logo-light.svg` | Mark + wordmark, navy text, for light backgrounds |
| `logo-dark.svg` | Mark + wordmark, white text, for dark backgrounds |
| `logo-tagline-dark.svg` | Same with the tagline, for social previews |
| `mark.svg` | Mark only |
| `icon.svg` | Simplified mark on a navy tile for favicons and avatars (64 px grid, reads at 16 px) |

Colours: navy `#0b2233`, blues `#1d4f7a` → `#4fc3c8`, accent teal
`#3fb8c4`, nodes white. The wordmark uses the system sans-serif stack
(Inter, Segoe UI, Helvetica Neue, Arial); outline the text before print.

The files are generated; the geometry lives in the arch parameters at the
top of the generator that produced them (centre 200,190; radii 132 and
88; 8 outer and 6 inner segments). `web/static/favicon.svg` and
`web/static/logo.svg` are copies of `icon.svg` and `mark.svg`.
