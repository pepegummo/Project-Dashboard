# ── Stage 1: Build ─────────────────────────────────────────────────────────
FROM node:20-alpine AS builder
WORKDIR /app

COPY package*.json ./
RUN npm ci --prefer-offline

COPY . .

# ⚠  Vite bakes VITE_* variables into the JS bundle at build time.
#    Pass them as --build-arg in docker compose / docker build.
#    Runtime env vars (docker run -e) have NO effect on the static bundle.
ARG VITE_API_URL=""
ARG VITE_WS_URL="ws://localhost:5173/ws"
ENV VITE_API_URL=${VITE_API_URL}
ENV VITE_WS_URL=${VITE_WS_URL}

RUN npm run build

# ── Stage 2: Serve ─────────────────────────────────────────────────────────
FROM nginx:1.27-alpine AS runner

# Copy built SPA
COPY --from=builder /app/dist /usr/share/nginx/html

# Copy Nginx config (includes /api and /ws reverse-proxy rules)
COPY nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80
CMD ["nginx", "-g", "daemon off;"]
