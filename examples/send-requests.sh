#!/usr/bin/env bash
# Populate the request console for a server running examples/user.http.
set -euo pipefail

usage() {
    cat <<'HELP'
Usage: examples/send-requests.sh [-p PORT]

Send sample traffic to http://127.0.0.1:PORT using examples/user.http routes.
Port precedence: -p PORT, MOCK_PORT, then 8080.

Start the server first:
  mock examples/user.http
  examples/send-requests.sh

Custom port:
  mock -p 9000 examples/user.http
  examples/send-requests.sh -p 9000

Requires Bash and curl. Response bodies are hidden; inspect them in the UI.
The two POST requests exercise the rotating 201/400 responses. Their order
may differ if the server has already handled POST /users requests.
HELP
}

trim() {
    local value=$1
    value=${value#"${value%%[![:space:]]*}"}
    value=${value%"${value##*[![:space:]]}"}
    printf '%s' "$value"
}

port=$(trim "${MOCK_PORT:-}")
port=${port:-8080}
while getopts ':p:h' option; do
    case "$option" in
        p) port=$OPTARG ;;
        h) usage; exit 0 ;;
        :) printf 'Missing value for -%s\n' "$OPTARG" >&2; usage >&2; exit 2 ;;
        \?) printf 'Unknown option: -%s\n' "$OPTARG" >&2; usage >&2; exit 2 ;;
    esac
done
shift "$((OPTIND - 1))"
if (( $# )); then
    printf 'Unexpected argument: %s\n' "$1" >&2
    usage >&2
    exit 2
fi

port=$(trim "$port")
if [[ ! $port =~ ^[0-9]+$ ]]; then
    printf 'Invalid port: %s (expected an integer from 1 to 65535)\n' "$port" >&2
    exit 2
fi
# Strip leading zeros before arithmetic, avoiding octal parsing and overflow.
port=${port#"${port%%[!0]*}"}
if [[ -z $port || ${#port} -gt 5 ]] || (( port > 65535 )); then
    printf 'Port must be between 1 and 65535.\n' >&2
    exit 2
fi
if ! command -v curl >/dev/null 2>&1; then
    printf 'curl is required to send sample requests.\n' >&2
    exit 1
fi

base_url="http://127.0.0.1:$port"
request() {
    local method=$1 path=$2
    shift 2
    printf '%-7s %-25s' "$method" "$path"
    # HTTP errors are useful UI data; only transport failures stop the script.
    if ! curl --silent --show-error --connect-timeout 3 --max-time 15 \
        --output /dev/null --write-out 'HTTP %{http_code} (%{time_total}s)\n' \
        --request "$method" --header 'X-Demo-Source: send-requests.sh' \
        "$@" "$base_url$path"; then
        printf 'Request failed. Is mock running with examples/user.http on port %s?\n' "$port" >&2
        exit 1
    fi
}

printf 'Sending sample traffic to %s\n\n' "$base_url"
request GET /status
request GET /
request GET /index.html
request GET /index2.html
request GET /users
request GET /users/42
request GET /users/7
request POST /users --header 'Content-Type: application/json' --data '{
  "name": "Alex Morgan",
  "email": "alex@example.test",
  "roles": ["editor", "reviewer"],
  "profile": {"city": "Seattle", "notifications": true}
}'
request POST /users --header 'Content-Type: application/json' --data '{
  "name": "Sam Rivera",
  "email": "sam@example.test",
  "roles": ["viewer"],
  "profile": {"city": "Portland", "notifications": false}
}'
request GET '/names?type=cat'
request GET '/names?type=dog'
request DELETE /users/7
request GET /chords
request GET /chords2
request GET /delay
printf '\nDone. Open %s/mock/ to inspect the traffic (or your custom -l UI path).\n' "$base_url"
