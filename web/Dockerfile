FROM node:24.21.0-alpine AS build
WORKDIR /src
RUN corepack enable && corepack prepare pnpm@11.26.0 --activate
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY web/package.json ./web/package.json
RUN pnpm install --frozen-lockfile
COPY web/ ./web/
WORKDIR /src/web
RUN pnpm build

FROM node:24.21.0-alpine
WORKDIR /app
ENV HOST=0.0.0.0 PORT=3000
COPY --from=build --chown=node:node /src/web/.output ./.output
USER node
EXPOSE 3000
CMD ["node", ".output/server/index.mjs"]
