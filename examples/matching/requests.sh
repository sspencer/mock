#!/usr/bin/env bash
# Exercise every route in examples/matching/*.http and print what each returns.
set -euo pipefail

usage() {
    cat <<'HELP'
Usage: examples/matching/requests.sh [-p PORT]

Send one request per scenario in examples/matching/*.http to
http://127.0.0.1:PORT and print the status and response body.
Port precedence: -p PORT, MOCK_PORT, then 8080.

Start the server first:
  mock examples/matching/*.http
  examples/matching/requests.sh

Requires Bash and curl. Every request also appears in the admin UI
(http://127.0.0.1:8081/), where unmatched requests explain why.
HELP
}

port=${MOCK_PORT:-8080}
while getopts ':p:h' option; do
    case "$option" in
        p) port=$OPTARG ;;
        h) usage; exit 0 ;;
        :) printf 'Missing value for -%s\n' "$OPTARG" >&2; usage >&2; exit 2 ;;
        \?) printf 'Unknown option: -%s\n' "$OPTARG" >&2; usage >&2; exit 2 ;;
    esac
done
if [[ ! $port =~ ^[0-9]+$ ]] || (( 10#$port < 1 || 10#$port > 65535 )); then
    printf 'Invalid port: %s (expected 1-65535)\n' "$port" >&2
    exit 2
fi
command -v curl >/dev/null 2>&1 || { printf 'curl is required.\n' >&2; exit 1; }

base_url="http://127.0.0.1:$((10#$port))"
signature="sha256=$(printf '%064d' 0)"

# show DESCRIPTION METHOD PATH [curl args...]
show() {
    local description=$1 method=$2 path=$3 response status body
    shift 3
    if ! response=$(curl --silent --show-error --connect-timeout 3 --max-time 10 \
        --request "$method" --write-out '\n%{http_code}' "$@" "$base_url$path"); then
        printf 'Request failed. Is mock running with examples/matching/*.http on port %s?\n' "$port" >&2
        exit 1
    fi
    status=${response##*$'\n'}
    body=${response%$'\n'*}
    # Collapse whitespace so each response fits on one line.
    body=$(printf '%s' "$body" | tr -s '[:space:]' ' ')
    printf '  %-48s %s  %.90s\n' "$description" "$status" "$body"
}

section() { printf '\n%s\n' "$1"; }

json=(--header 'Content-Type: application/json')

section 'Headers (headers.http)'
show 'Bearer token, captured into the body'  GET /account/me --header 'Authorization: Bearer abc123'
show 'No Authorization header'               GET /account/me
show 'Basic auth instead of Bearer'          GET /account/me --header 'Authorization: Basic dXNlcjpwYXNz'
show 'Accept: text/csv (case-insensitive)'   GET /account/report --header 'Accept: TEXT/CSV'
show 'Default Accept'                        GET /account/report
show 'X-Api-Version: 2'                      GET /account/settings --header 'X-Api-Version: 2'
show 'No version header'                     GET /account/settings
show 'X-Api-Version: 9'                      GET /account/settings --header 'X-Api-Version: 9'
show 'X-Tenant: acme-eu'                     GET /account/usage --header 'X-Tenant: acme-eu'
show 'X-Tenant: Not Valid'                   GET /account/usage --header 'X-Tenant: Not Valid'

section 'JSON bodies (json.http)'
show 'Test order (whole-document @json)'     POST /orders "${json[@]}" \
    --data '{"customer":{"name":"Test","vip":false},"test":true,"items":[]}'
show 'Express shipping, echoes customer name' POST /orders "${json[@]}" \
    --data '{"customer":{"name":"Avery"},"shipping":{"method":"express"},"items":[{"sku":"TEA-001","qty":2}]}'
show 'VIP customer (boolean true)'           POST /orders "${json[@]}" \
    --data '{"customer":{"vip":true},"items":[{"sku":"TEA-002","qty":1}]}'
show 'Gift card in any item, amount captured' POST /orders "${json[@]}" \
    --data '{"items":[{"sku":"TEA-001","qty":1},{"sku":"GIFT-50","qty":1}]}'
show 'Standard order, items copied back'     POST /orders "${json[@]}" \
    --data '{"items":[{"sku":"TEA-003","qty":3}]}'
show 'No items'                              POST /orders "${json[@]}" --data '{"customer":{"name":"Avery"}}'
show 'Not JSON'                              POST /orders "${json[@]}" --data 'items=TEA-001'
show 'Numeric order id'                      GET /orders/1001
show 'Non-numeric order id'                  GET /orders/latest
show 'qty 3'                                 PATCH /orders/1001 "${json[@]}" --data '{"qty":3}'
show 'qty 3.5'                               PATCH /orders/1001 "${json[@]}" --data '{"qty":3.5}'

section 'Form bodies (forms.http)'
show 'client_credentials, client captured'   POST /auth/token --data 'grant_type=client_credentials&client_id=acme-web&scope=read+write'
show 'password grant'                        POST /auth/token --data 'grant_type=password&username=avery&password=hunter2'
show 'refresh_token grant'                   POST /auth/token --data 'grant_type=refresh_token&refresh_token=rt_9f8e7d6c5b'
show 'No grant_type'                         POST /auth/token --data 'client_id=acme-web'
show 'Unsupported grant'                     POST /auth/token --data 'grant_type=device_code'
show 'Contact form from a person'            POST /auth/contact --data 'name=Avery&email=avery@example.test&message=Hello&website='
show 'Contact form from a bot (honeypot)'    POST /auth/contact --data 'name=Bot&email=bot@spam.test&message=Buy&website=spam.test'
show 'Contact form without an email'         POST /auth/contact --data 'name=Avery&message=Hello'

section 'Paths and queries (paths-and-queries.http)'
show 'Numeric product id'                    GET /catalog/products/7
show 'Product slug, parts captured'          GET /catalog/products/green-tea-250g
show 'Unknown product id'                    GET /catalog/products/Green_Tea
show 'Search with q and page'                GET '/catalog/search?q=green&page=2'
show 'Search with q only'                    GET '/catalog/search?q=green'
show 'Search without q'                      GET /catalog/search
show 'Search with a bad page'                GET '/catalog/search?q=green&page=zero'
show 'Image under a nested path'             GET /catalog/images/teas/green/hero.webp
show 'Non-image file'                        GET /catalog/images/teas/notes.txt
show 'Anything else under /catalog'          GET /catalog/brands/acme

section 'Raw bodies and webhooks (webhooks.http)'
show 'Signed payment.succeeded'              POST /hooks/payments "${json[@]}" --header "X-Signature: $signature" \
    --data '{"type":"payment.succeeded","data":{"id":"pay_123"}}'
show 'Signed payment.canceled'               POST /hooks/payments "${json[@]}" --header "X-Signature: $signature" \
    --data '{"type":"payment.canceled","data":{"id":"pay_124","reason":"customer request"}}'
show 'Signed, other event type'              POST /hooks/payments "${json[@]}" --header "X-Signature: $signature" \
    --data '{"type":"payout.created","data":{"id":"po_1"}}'
show 'Unsigned'                              POST /hooks/payments "${json[@]}" --data '{"type":"payment.succeeded"}'
show 'Malformed signature'                   POST /hooks/payments "${json[@]}" --header 'X-Signature: md5=abc' \
    --data '{"type":"payment.succeeded"}'
show 'Text containing PING on a later line'  POST /hooks/text --header 'Content-Type: text/plain' --data-binary $'hello\nPING\nbye'
show 'XML status, value captured'            POST /hooks/text --header 'Content-Type: application/xml' \
    --data '<report><status>healthy</status></report>'
show 'Empty body'                            POST /hooks/text
show 'Anything else'                         POST /hooks/text --header 'Content-Type: text/plain' --data 'ping'
