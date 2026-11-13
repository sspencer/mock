# Troubleshooting

Answers to the problems people hit most often, with the command or page that
settles each one.

## My request gets a 404

A `404` with no route behind it means no route accepted the request. Ask
`mock` why, without sending anything:

```sh
mock match api.http GET '/users?role=guest'
```

```text
GET /users?role=guest
  no route matches

closest routes:
  "Admin users" (api.http:12)  GET /users
    - Query role: expected "admin"; received ["guest"]
```

The same list is in the admin UI: select the request in the log and **Why didn’t
this request match?** shows the closest routes, with the failed part of each
reason highlighted. **Similar misses** filters the log to every request that
missed the same route (`near:NAME`). See
[Which route answers a request](cli.md#which-route-answers-a-request) for the
`mock match` flags (`-H` headers, `-d` bodies, `-port`).

## Reading near-miss reasons

Each reason names the part of the request that failed and what the route
wanted. The common ones:

| Reason | What to check |
|--------|---------------|
| `Expected method GET; received POST` | The request line's method. `GET,HEAD` or `*` accept more than one |
| `Path "/nope" does not match "/users"` | The path, including a trailing slash and `:name` segments |
| `Served on port 9000; request arrived on port 8080` | The route serves another port (a port in its URL, `@port`, or `-p`). See [Multiple ports](matching.md#multiple-ports) |
| `Query role: required value is missing` | A query value written in the request target, or an `@query.role` matcher |
| `Header X-Tenant: ...` | An `@header.X-Tenant` matcher. Headers after the request line are *response* headers and never match anything |
| `JSON customer.id: ...`, `Form ...`, `XML ...`, `Body: ...` | A body matcher. The request needs the right `Content-Type`, and a body larger than `--max-body` (1 MiB by default) matches no body matcher |
| `Host: ...` | An [`@match.host`](matching.md#host-matching) matcher. `curl -H 'Host: name'` sets it |
| `Answered its @times = 2 already` | The route used up its answers. `POST /reset` starts it over. See [Routes that answer a few times](matching.md#routes-that-answer-a-few-times) |
| `Scenario order is in state ...` | The route waits for another [scenario](matching.md#scenarios) state |
| `Skipped by @match.chance` | The route answers only a share of requests, and this one fell outside it |
| `Switched off at run time` | A route switch in the admin UI, or a `POST /faults` override. `DELETE /faults` clears them |

Matcher values are exact unless they say otherwise: `~regex` must match the
whole value, so `~json` does not match `application/json` but `~.*json` does.
See [Matcher values](matching.md#matcher-values).

## The wrong route answers

The first route in the file that accepts the request wins, so a general route
above a specific one hides it. `mock` warns at startup:

```text
level=WARN msg="route can never match: earlier route \"Users\" (api.http:1) matches every request it would; put more specific routes first"
```

Move the specific route up, or narrow the general one. `mock match` prints
the route that answers, and `mock routes` prints the table in the order
routes are tried. See [Route order](matching.md#route-order).

## Port already in use

```text
server failed: listen tcp 127.0.0.1:8080: bind: address already in use
```

Another process, often an earlier `mock`, holds the port. Pick other ports
with `-p` (mock port) and `-a` (admin port), or find the process with
`lsof -i :8080`. `-a 0` picks a free admin port, and `mock run` picks free
ports for both. A route
that names the admin port is a load error; change the route's port, set `-a`,
or pass `-x` to run without the admin UI.

## A reload failed

When an edited file no longer loads, `mock` keeps serving the previous routes,
prints the error with its `file:line`, and shows it in a banner in the admin
UI (and as `error` in `GET /state`). Fix the file and save again. To see
every problem at once:

```sh
mock validate -strict api.http
```

`validate` exits `1` on errors, or on warnings with `-strict`, so it also
works as a pre-commit check. See
[Validating request files](cli.md#validating-request-files).

A reload cannot open new ports: a route that moves to a port `mock` is not
listening on fails the reload with "restart mock to serve new ports".

## Changes are not picked up

Some filesystems never send change events: Docker Desktop bind mounts, some
network filesystems, and editors that save through another directory. Use
`-i 1s` to poll, `kill -HUP <pid>`, or `POST /reload`. See
[Reloading](cli.md#reloading).

## A placeholder came out literally

A malformed tag, such as `{{$name` without its closing braces, stays as text,
and `mock validate` warns about it. A JSON body that would not parse once its
placeholders are filled in is also a warning. Check the name against
[Placeholders](placeholders.md#placeholders): request values are
`{{$path.id}}`, `{{$query.q}}`, `{{$header.Name}}` and `{{$json.field}}`,
and file variables are `{{name}}` without the `$`.

## Requests are missing from the log

The request log keeps the latest 1,000 requests; raise it with `--log-size`,
or keep everything with `--journal FILE`. `GET /requests` leaves out webhook
callbacks unless asked for them with `callbacks=true`. See
[Reading the request log](admin-ui.md#reading-the-request-log).
