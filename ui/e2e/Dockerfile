FROM mcr.microsoft.com/playwright:v1.63.0-noble
WORKDIR /src
RUN corepack enable && corepack prepare pnpm@10.33.2 --activate
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY ui/package.json ./ui/package.json
RUN pnpm install --frozen-lockfile
COPY ui ./ui
WORKDIR /src/ui
CMD ["pnpm", "exec", "playwright", "test", "--config=e2e/playwright.config.ts"]
