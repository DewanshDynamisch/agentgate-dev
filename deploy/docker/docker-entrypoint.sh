#!/bin/sh
set -e

# Construct CSP connect-src from env
CONNECT_SRC="'self'"
if [ -n "$AGENTGATE_UI_API_URL" ]; then
    CONNECT_SRC="$CONNECT_SRC $AGENTGATE_UI_API_URL"
fi
if [ -n "$AGENTGATE_UI_OIDC_AUTHORITY" ]; then
    CONNECT_SRC="$CONNECT_SRC $AGENTGATE_UI_OIDC_AUTHORITY ${AGENTGATE_UI_OIDC_AUTHORITY}/ https://keycloak:8443"
fi

export CSP_HEADER="default-src 'self'; connect-src $CONNECT_SRC; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self';"

# Render nginx server configuration
envsubst '$CSP_HEADER' < /etc/nginx/templates/default.conf.template > /etc/nginx/conf.d/default.conf

# Render runtime config.js
envsubst '${AGENTGATE_UI_API_URL} ${AGENTGATE_UI_OIDC_AUTHORITY} ${AGENTGATE_UI_OIDC_CLIENT_ID}' \
    < /usr/share/nginx/html/config.js.template \
    > /usr/share/nginx/html/config.js

exec "$@"
