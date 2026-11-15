# Veylo Web — multi-stage build (Next.js)
FROM node:22-alpine AS deps

WORKDIR /app
RUN corepack enable

# Copy only manifests first for layer caching.
COPY apps/web/package.json apps/web/pnpm-lock.yaml* ./apps/web/
COPY package.json pnpm-workspace.yaml ./

RUN pnpm install --frozen-lockfile=false

FROM node:22-alpine AS build

WORKDIR /app
RUN corepack enable

COPY --from=deps /app/node_modules ./node_modules
COPY --from=deps /app/apps/web/node_modules ./apps/web/node_modules
COPY . .

ENV NEXT_TELEMETRY_DISABLED=1

RUN pnpm --filter @veylo/web build

FROM node:22-alpine AS run

WORKDIR /app
RUN corepack enable

ENV NODE_ENV=production \
    NEXT_TELEMETRY_DISABLED=1 \
    PORT=3000

COPY --from=build /app/apps/web/.next/standalone ./
COPY --from=build /app/apps/web/.next/static ./apps/web/.next/static
COPY --from=build /app/apps/web/public ./apps/web/public

EXPOSE 3000

CMD ["node", "apps/web/server.js"]
