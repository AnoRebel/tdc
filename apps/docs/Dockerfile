# The public documentation site.
#
# Built from the repository root (build context "."), because the guides are
# the repository's docs/ directory and the site reads the help centre's own
# catalogue from packages/ui:
#
#   docker build -f apps/docs/Dockerfile --build-arg DOCS_VERSION=dev .
#
# One image per version: DOCS_VERSION names the branch the image was built
# from, which is also the branch its Studio commits to and the one its links
# into the repository point at. Everything secret is given to the running
# container, never to the build.

FROM node:24-bookworm-slim AS build

# Bun installs the workspace; Node runs the build, because the content layer
# indexes the guides with node:sqlite.
COPY --from=oven/bun:1-debian /usr/local/bin/bun /usr/local/bin/bun

WORKDIR /src

# Every workspace manifest, so the frozen lockfile matches, and an install
# cached independently of the guides changing.
COPY package.json bun.lock ./
COPY apps/web/package.json apps/web/
COPY apps/docs/package.json apps/docs/
COPY apps/desktop/frontend/package.json apps/desktop/frontend/
COPY packages/types/package.json packages/types/
COPY packages/ui/package.json packages/ui/
# --ignore-scripts: the root postinstall installs git hooks, which needs git
# and means nothing in an image.
# @tdc/ui as well: the site reads the help centre's catalogue from it, and that
# module imports its icons.
RUN bun install --frozen-lockfile --ignore-scripts --filter @tdc/docs --filter @tdc/ui

COPY docs/ docs/
COPY packages/ui/ packages/ui/
COPY apps/docs/ apps/docs/

ARG DOCS_VERSION=main
ARG DOCS_REPOSITORY=AnoRebel/tdc
ENV DOCS_VERSION=$DOCS_VERSION \
    DOCS_REPOSITORY=$DOCS_REPOSITORY

RUN cd apps/docs && bun run build

FROM node:24-bookworm-slim

WORKDIR /app
# The server writes its content index under .data on first start.
RUN chown node:node /app
USER node

COPY --from=build --chown=node:node /src/apps/docs/.output ./.output

ARG DOCS_VERSION=main
ENV NODE_ENV=production \
    HOST=0.0.0.0 \
    PORT=3000 \
    DOCS_VERSION=$DOCS_VERSION

EXPOSE 3000
CMD ["node", ".output/server/index.mjs"]
