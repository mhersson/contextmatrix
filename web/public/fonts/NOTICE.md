# Third-party font notices

ContextMatrix's web UI self-hosts three open-source font families. The
`.woff2` binaries are served from the Go binary via `//go:embed all:dist`,
so every deployment ships the fonts directly — no external CDN or CSS
imports at runtime.

All three fonts are licensed under the **SIL Open Font License 1.1**. The
full license text is downloaded from each project's upstream repository
by `scripts/fontfetch.py` and written next to the font files:

| Family         | Upstream                                         | License file                     |
| -------------- | ------------------------------------------------ | -------------------------------- |
| Fraunces       | https://github.com/undercasetype/Fraunces        | `fraunces/OFL.txt`               |
| Geist          | https://github.com/vercel/geist-font             | `geist/OFL.txt`                  |
| JetBrains Mono | https://github.com/JetBrains/JetBrainsMono       | `jetbrains-mono/OFL.txt`         |

To verify file integrity, `scripts/fontfetch.py` maintains
`manifest.json` in this directory with SHA-256 digests for every
downloaded `.woff2`. Re-running the script checks existing files against
the manifest and re-downloads on mismatch.

To regenerate (e.g. after adding a weight or a new family), edit
`scripts/fontfetch.py` and run it from the repo root:

```
python3 scripts/fontfetch.py
```
