# Binary releases

Both hosts build independently from their own checkout. Pushing a `v*` tag
publishes a release on that host containing `attic-<tag>-linux-amd64.tar.gz` and a
SHA-256 checksum. Tags containing a hyphen are marked as prereleases. Retrying a
tag workflow replaces its release assets (Forgejo recreates the release, including
its generated notes). Builds on `master` and manual branch
runs validate the package without publishing a release. GitHub also validates
pull requests and retains build artifacts for download from the Actions run.

To release the same commit on both configured remotes:

```sh
git tag v1.0.0
git push origin v1.0.0
git push github v1.0.0
```

Choose the actual version before running these commands. The tagged commit must
contain the workflows. Branch mirroring alone may not propagate tags; push tags
to both hosts or enable tag mirroring. Manual runs on a `v*` tag also publish.

Forgejo uses an existing runner with the `ubuntu-latest` label by default. If your
runner has another label, set the repository Actions variable
`LINUX_RUNNER_LABEL`. The runner must support Docker job containers. The job uses
the Node 22 Bookworm image for bash, git and the action runtime; the release
action installs jq if needed. It must be able to download the public container
image, actions, Go, Node, and package dependencies. Enable repository Actions if needed.
The built-in job token publishes to the current repository; no personal access
token or cross-host credentials are needed.

GitHub uses hosted runners and its own job token, granting release write access
only to the publishing job. The workflows contain no private server addresses,
runner names, network topology, or credentials. The private host URL is resolved
from the job context only on that host. No artifacts or logs are transferred from
the private host to GitHub. The generic `.forgejo` workflow remains visible if
the same source tree is pushed to GitHub; it reveals use of Forgejo, but no
instance-specific configuration. This does not scrub existing repository history.

To build locally with Go 1.24.6, Node 22.22.0, and the pnpm version pinned in
`package.json`:

```sh
bash scripts/build-release.sh dev
```

The script installs locked UI dependencies, builds and embeds the UI, runs Go
tests, cross-compiles with CGO disabled, and packages only the executable,
migrations and installation guide. Database integration tests still require
`ATTIC_TEST_DATABASE_URL`; the release jobs do not provision a database.
See [binary installation](binary-install.md) for runtime requirements.
