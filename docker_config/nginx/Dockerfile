FROM node:22-alpine3.24 AS frontend

WORKDIR /src

RUN corepack enable && corepack prepare pnpm@10.11.0 --activate

COPY front/package.json front/pnpm-lock.yaml front/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile

COPY front/ ./
RUN pnpm run build

FROM nginx:1.30.5-alpine3.24

USER root
RUN apk upgrade --no-cache

COPY docker_config/nginx/nginx.conf /etc/nginx/nginx.conf
COPY docker_config/nginx/http.conf.template /etc/nginx/http.conf.template
COPY docker_config/nginx/proxy_params /etc/nginx/proxy_params
COPY docker_config/nginx/proxy_params_ws /etc/nginx/proxy_params_ws
COPY docker_config/nginx/api_limits.conf /etc/nginx/api_limits.conf
COPY docker_config/nginx/05-forcad-proxy-proto.sh /docker-entrypoint.d/05-forcad-proxy-proto.sh

COPY --from=frontend /src/dist/ /var/www/front/
COPY teams_logo/ /var/www/front/team-logos/

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
