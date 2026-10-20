# The front end.
#
# Built from the repository root, like the others:
#   docker build -f deploy/web/Dockerfile -t ballast-web .

FROM node:22-alpine AS deps

WORKDIR /app

# The lockfile first, and `npm ci` rather than `npm install`: a build that
# resolves versions of its own is a build that cannot be repeated.
COPY web/package.json web/package-lock.json ./
RUN npm ci

FROM node:22-alpine AS build

WORKDIR /app

COPY --from=deps /app/node_modules ./node_modules
COPY web/ ./

# Telemetry is off here rather than at runtime: the build is where it phones
# home, and a build that talks to a third party about this project is not
# something to leave on by default.
ENV NEXT_TELEMETRY_DISABLED=1
RUN npm run build

FROM node:22-alpine AS runtime

WORKDIR /app

ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1
# Next binds to localhost by default, which inside a container means nothing
# outside it can connect.
ENV HOSTNAME=0.0.0.0
ENV PORT=3000

# Three directories, and only three. `standalone` carries the server and the
# subset of node_modules it loads; the other two are not in it, by design.
COPY --from=build --chown=node:node /app/.next/standalone ./
COPY --from=build --chown=node:node /app/.next/static ./.next/static
COPY --from=build --chown=node:node /app/public ./public

# The unprivileged user the node images already carry.
USER node

EXPOSE 3000

CMD ["node", "server.js"]
