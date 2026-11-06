#!/usr/bin/env bash
# End-to-end smoke test: build mock, serve the examples, hit mock and admin
# endpoints, exercise multi-port routing and hot reload. Exits non-zero on
# the first failure. Ports 18080-18199 must be free.
set -euo pipefail

root=$(git -C "$(dirname "$0")" rev-parse --show-toplevel)
work=$(mktemp -d "${TMPDIR:-/tmp}/mock-smoke.XXXXXX")
pids=()
cleanup() {
    for p in "${pids[@]}"; do kill "$p" 2>/dev/null || true; done
    wait 2>/dev/null || true
    rm -rf "$work"
}
trap cleanup EXIT

pass=0
ok()   { pass=$((pass + 1)); printf 'ok   %s\n' "$1"; }
fail() { printf 'FAIL %s\n' "$1" >&2; for f in "$work"/*.log; do echo "--- $f" >&2; cat "$f" >&2; done; exit 1; }

# expect NAME CURL_ARGS... -- checks HTTP status in $want and optional body substring in $body
check() {
    local name=$1 want=$2 body=$3; shift 3
    local out code
    out=$(curl -sS -m 5 -w '\n%{http_code}' "$@") || fail "$name (curl error)"
    code=${out##*$'\n'}
    out=${out%$'\n'*}
    [[ $code == "$want" ]] || fail "$name: status $code, want $want"
    [[ -z $body || $out == *"$body"* ]] || fail "$name: body missing '$body'"
    ok "$name"
}

wait_up() {
    local url=$1
    for _ in $(seq 50); do curl -s -o /dev/null -m 1 "$url" && return 0; sleep 0.1; done
    fail "server at $url did not start"
}

cd "$root"
go build -trimpath -ldflags "-X main.version=smoke" -o "$work/mock" . || fail "build"
ok "build"
[[ $("$work/mock" -version) == *smoke* ]] || fail "-version"
ok "-version"

"$work/mock" validate examples/*.http examples/matching/*.http > "$work/validate.log" 2>&1 || fail "validate examples"
ok "validate examples"

# --- single port: examples/user.http + static.http ------------------------
"$work/mock" -p 18080 -a 18081 examples/user.http examples/static.http > "$work/user.log" 2>&1 &
pids+=($!)
wait_up http://127.0.0.1:18081/state

check "GET /users (external file)"   200 '"Pasta Dough"' http://127.0.0.1:18080/users
check "GET / (html)"                  200 '<'             http://127.0.0.1:18080/
check "GET /users?role=admin (query)"  200 '"role": "admin"' 'http://127.0.0.1:18080/users?role=admin'
check "POST /users echoes JSON"       201 '"name": "Ann"'  -X POST -d '{"name":"Ann","email":"a@b.test"}' http://127.0.0.1:18080/users
check "POST /users taken email"       409 '"success": false' -X POST -d '{"name":"x","email":"pasta@dough.com"}' http://127.0.0.1:18080/users
check "POST /users without email"     400 '"success": false' -X POST -d '{"name":"x"}' http://127.0.0.1:18080/users
check "GET /users/:id"                200 '"id": 7'       http://127.0.0.1:18080/users/7
check "GET /users/:id non-numeric"    400 ''              http://127.0.0.1:18080/users/me
check "unmatched route"               404 ''              http://127.0.0.1:18080/nope
check "admin UI page"                 200 'id="mock-config"' http://127.0.0.1:18081/
check "admin /state"                  200 '"revision":1'  http://127.0.0.1:18081/state
check "admin /routes"                 200 '"port":18080'  http://127.0.0.1:18081/routes
check "admin /clear rejects bare POST" 403 ''             -X POST http://127.0.0.1:18081/clear
check "admin /clear"                  204 ''              -X POST -H 'X-Requested-With: smoke' http://127.0.0.1:18081/clear

# --- multi-port + hot reload: services.http on shifted ports -------------
sed -e 's/:8080/:18090/; s/:9090/:18091/; s/@port = 9100/@port = 18092/' examples/services.http > "$work/services.http"
"$work/mock" -a 18093 "$work/services.http" > "$work/services.log" 2>&1 &
pids+=($!)
wait_up http://127.0.0.1:18093/state

check "users port /users"            200 'Rusty'      http://127.0.0.1:18090/users
check "auth port /token"             200 '"token"'    -X POST http://127.0.0.1:18091/token
check "@port /metrics"               200 'requests_total' http://127.0.0.1:18092/metrics
check "route only on its own port"   404 ''           http://127.0.0.1:18091/users
check "health per port"              200 'auth ok'    http://127.0.0.1:18091/health

printf '\n### Reloaded\nGET http://localhost:18090/reloaded\n\nfresh\n' >> "$work/services.http"
for _ in $(seq 50); do curl -s -m 1 http://127.0.0.1:18093/state | grep -q '"revision":2' && break; sleep 0.1; done
check "hot reload adds route"        200 'fresh'      http://127.0.0.1:18090/reloaded

printf '\n### New port\nGET http://localhost:18199/x\n\nx\n' >> "$work/services.http"
for _ in $(seq 50); do curl -s -m 1 http://127.0.0.1:18093/state | grep -q '"error"' && break; sleep 0.1; done
check "reload on unopened port rejected" 200 '18199'  http://127.0.0.1:18093/state
check "old routes kept after bad reload" 200 'fresh'  http://127.0.0.1:18090/reloaded

printf '\nsmoke: %d checks passed\n' "$pass"
