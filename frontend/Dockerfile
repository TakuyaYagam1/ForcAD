# syntax=docker/dockerfile:1.7

FROM node:26.8.2-alpine3.24@sha256:ef24c5053d50fdc3e4e56eb4e7ddb7861874ab0fdc797046ba897581deb8e868 AS frontend

WORKDIR /workspace

RUN npm install --global pnpm@12.11.2

COPY frontend/package.json frontend/pnpm-lock.yaml frontend/pnpm-workspace.yaml ./frontend/

WORKDIR /workspace/frontend

RUN --mount=type=cache,target=/root/.local/share/pnpm/store \
    pnpm install --frozen-lockfile

WORKDIR /workspace

COPY frontend/ ./frontend/
COPY public/ ./public/

WORKDIR /workspace/frontend

ARG VITE_BRAND_NAME=ForcAD
ARG VITE_BRAND_LOGO=/brand/forcad-mark.svg
ARG VITE_BRAND_PRIMARY=#5B8DEF
ARG VITE_BRAND_ACCENT=#38B7B0

ENV VITE_BRAND_NAME=${VITE_BRAND_NAME} \
    VITE_BRAND_LOGO=${VITE_BRAND_LOGO} \
    VITE_BRAND_PRIMARY=${VITE_BRAND_PRIMARY} \
    VITE_BRAND_ACCENT=${VITE_BRAND_ACCENT}

RUN pnpm run build

FROM nginx:1.30.5-alpine3.24@sha256:0985e772fb9f729e6fa0980da05fca5d9c468e870eed43071545afa9d2e27d94 AS web

USER root

RUN apk upgrade --no-cache

COPY deployment/nginx/nginx.conf /etc/nginx/nginx.conf
COPY deployment/nginx/http.conf.template /etc/nginx/http.conf.template
COPY deployment/nginx/proxy_params /etc/nginx/proxy_params
COPY deployment/nginx/proxy_params_ws /etc/nginx/proxy_params_ws
COPY deployment/nginx/api_limits.conf /etc/nginx/api_limits.conf
COPY deployment/docker/scripts/nginx/05-forcad-proxy-proto.sh /docker-entrypoint.d/05-forcad-proxy-proto.sh

COPY --from=frontend /workspace/frontend/dist/ /var/www/front/

ARG DNS_RESOLVER=127.0.0.11
ENV DNS_RESOLVER=${DNS_RESOLVER}

RUN envsubst '${DNS_RESOLVER}' < /etc/nginx/http.conf.template > /etc/nginx/http.conf \
    && rm -f /etc/nginx/conf.d/default.conf \
    && mkdir -p /tmp/nginx/client_temp /tmp/nginx/proxy_temp \
        /tmp/nginx/fastcgi_temp /tmp/nginx/uwsgi_temp /tmp/nginx/scgi_temp \
    && chown -R nginx:nginx /tmp/nginx \
    && printf 'proxy_set_header X-Forwarded-Proto http;\n' > /tmp/forcad-proxy-proto \
    && : > /tmp/forcad-trusted-proxies.conf \
    && chown nginx:nginx /tmp/forcad-proxy-proto /tmp/forcad-trusted-proxies.conf \
    && chmod 0644 /tmp/forcad-proxy-proto /tmp/forcad-trusted-proxies.conf \
    && chmod +x /docker-entrypoint.d/05-forcad-proxy-proto.sh \
    && nginx -t \
    && rm -f /tmp/nginx/nginx.pid

EXPOSE 8080

USER nginx
