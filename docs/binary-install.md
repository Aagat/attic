# Linux binary installation

The release archive contains a statically linked Linux x86-64 (amd64) executable,
the embedded browser UI, and the SQL migrations. It targets baseline x86-64 CPUs.

Verify and extract the archive (replace the version with your release):

```sh
sha256sum -c attic-v1.0.0-linux-amd64.tar.gz.sha256
tar -xzf attic-v1.0.0-linux-amd64.tar.gz
cd attic-v1.0.0-linux-amd64
```

Provide the configuration described in the project README through environment
variables. Configure your owner token, AI provider credentials, PostgreSQL
connection and writable artifact directory, and point Attic at the extracted
migrations. For example, using an OpenAI-compatible API provider:

```sh
export DATABASE_URL='postgres://attic:YOUR_PASSWORD@localhost:5432/attic?sslmode=disable'
export BEARER_TOKEN='REPLACE_WITH_A_LONG_RANDOM_OWNER_TOKEN'
export AI_PROVIDER='api'
export AI_BASE_URL='https://api.openai.com/v1'
export AI_API_KEY='REPLACE_WITH_YOUR_PROVIDER_KEY'
export MIGRATIONS_DIR="$PWD/migrations"
export ARTIFACT_ROOT="$PWD/data/artifacts"
export LISTEN_ADDRESS='127.0.0.1:8080'
mkdir -p "$ARTIFACT_ROOT"
./attic
```

Replace the placeholder credentials before starting. For the alternative ChatGPT
provider, follow the project README's login and configuration instructions.

PostgreSQL 14+ is required. The binary does not bundle processing tools: install
Chromium (set `BROWSER_EXECUTABLE`), Pandoc, XeLaTeX and the necessary fonts/TeX
packages, Poppler utilities, and Xvfb as required by the processing modes you use.
The project's Dockerfile lists the complete processing runtime dependencies.
For processing that needs a display, run under `xvfb-run -a ./attic`.

Attic applies migrations at startup. Back up your database before upgrading, and
replace the executable and migrations together.
