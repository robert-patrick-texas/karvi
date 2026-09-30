# karvi v0.24.0 artifact index

The release's artifacts are generated beside the source tree and published
as the assets of the GitHub release `karvi-v0.24.0`
(https://github.com/robert-patrick-texas/karvi/releases):

- `karvi-v0.24.0-source-linux-amd64.tar.gz` — the complete source with
  the vendored dependencies, this evidence, and the qualified Linux/amd64
  executables (both transports; one build);
- `karvi-v0.24.0-source-linux-amd64.tar.gz.sha256` — its checksum; and
- `karvi-v0.24.0-artifacts.sha256` — the aggregate delivery checksums.

Every release is installed as new from its bundle (`BUILD-HOWTO.md` §3 and
§10); no source patch is shipped, and between two public tags the source
difference is `git diff`. Releases before 0.24.0 were made from the
maintainer's private history and their bundles are not published.

The build workflow is `BUILD-HOWTO.md`; the qualification evidence is
`release/BUILD-RESULT.md` and `release/evidence/`.
