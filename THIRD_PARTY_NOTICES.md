# Third-party notices

This release of mcp includes Go modules used in its supported Linux, macOS, and Windows builds. Dependency license texts and any source material required by those licenses are included in [`THIRD_PARTY_LICENSES/`](THIRD_PARTY_LICENSES/). The Go standard-library/toolchain license is also included there. The project itself is licensed separately under AGPL-3.0-only; third-party components retain their own licenses.

| Component | License | Included target(s) | License/source reference |
|---|---|---|---|
| `github.com/99designs/keyring` | MIT | darwin, windows | [upstream](https://github.com/99designs/keyring/blob/v1.2.2/LICENSE) |
| `github.com/danieljoos/wincred` | MIT | windows | [upstream](https://github.com/danieljoos/wincred/blob/v1.1.2/LICENSE) |
| `github.com/dvsekhvalnov/jose2go` | MIT | darwin, windows | [upstream](https://github.com/dvsekhvalnov/jose2go/blob/v1.5.0/LICENSE) |
| `github.com/mtibben/percent` | MIT | darwin, windows | [upstream](https://github.com/mtibben/percent/blob/v0.2.1/LICENSE) |
| `golang.org/x/crypto` | BSD-3-Clause | darwin, linux, windows | [upstream](https://cs.opensource.google/go/x/crypto/+/v0.31.0:LICENSE) |
| `golang.org/x/sys/unix` | BSD-3-Clause | darwin, linux | [upstream](https://cs.opensource.google/go/x/sys/+/v0.29.0:LICENSE) |
| `golang.org/x/sys/windows` | BSD-3-Clause | windows | [upstream](https://cs.opensource.google/go/x/sys/+/v0.29.0:LICENSE) |
| `golang.org/x/term` | BSD-3-Clause | darwin, linux, windows | [upstream](https://cs.opensource.google/go/x/term/+/v0.28.0:LICENSE) |

The report was generated from the release build graph for `linux`, `darwin`, and `windows`. Assembly and cgo source can require manual inspection beyond automated package-level scanning; the license of each complete module was reviewed and the applicable files are preserved in the bundle.
