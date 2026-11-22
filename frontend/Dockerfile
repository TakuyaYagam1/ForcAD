# syntax=docker/dockerfile:1.29.0@sha256:572cebbc2b15e6f84c077742952674889cb63cd5e9b80abdffcbe587b6f19183

FROM node:26.13.1-alpine3.24@sha256:6bd9ab43c0ec0190ca7b2d193906cb8b7edf6e7f4afa43ddeccd6452710bffb5 AS frontend

WORKDIR /workspace

RUN npm install --global pnpm@12.21.0

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

FROM nginx:1.30.7-alpine3.24@sha256:caf4e1e032a20f9c3c5e28fc7f25ffd768ad8677b4975c54193075997b3a97e3 AS web

USER root

RUN apk upgrade --no-cache

COPY deployment/nginx/nginx.conf /etc/nginx/nginx.conf
COPY deployment/nginx/http.conf.template /etc/nginx/http.conf.template
COPY deployment/nginx/proxy_params /etc/nginx/proxy_params
COPY deployment/nginx/proxy_params_ws /etc/nginx/proxy_params_ws
COPY deployment/nginx/api_limits.conf /etc/nginx/api_limits.conf
COPY deployment/nginx/frontend_security_headers.conf /etc/nginx/frontend_security_headers.conf
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
    && printf 'default 1;\n' > /tmp/forcad-admin-network.conf \
    && : > /tmp/forcad-admin-deny.conf \
    && chown nginx:nginx /tmp/forcad-proxy-proto /tmp/forcad-trusted-proxies.conf \
        /tmp/forcad-admin-network.conf /tmp/forcad-admin-deny.conf \
    && chmod 0644 /tmp/forcad-proxy-proto /tmp/forcad-trusted-proxies.conf \
        /tmp/forcad-admin-network.conf /tmp/forcad-admin-deny.conf \
    && chmod +x /docker-entrypoint.d/05-forcad-proxy-proto.sh \
    && nginx -t \
    && rm -f /tmp/nginx/nginx.pid

EXPOSE 8080

USER nginx
