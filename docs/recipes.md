[Documentation](README.md) › Recipes

# Recipes

These combine features documented above for needs that come up often.

## GraphQL operations

GraphQL clients send every query to one URL, so match on the operation name in
the JSON body. Routes are tried in order, so a fallback without a matcher goes
last:

```http
### Get user
@json.operationName = GetUser
POST /graphql
Content-Type: application/json

{"data": {"user": {"id": "{{$json.variables.id}}", "name": "{{$name}}"}}}

### List orders
@json.operationName = ListOrders
POST /graphql
Content-Type: application/json

{"data": {"orders": []}}

### [400] Unknown operation
POST /graphql
Content-Type: application/json

{"errors": [{"message": "unknown operation"}]}
```

## Content negotiation

Match the `Accept` header with a regex. Regexes are anchored, so wrap the media
type in `.*`, and start with `(?i)` because clients differ in case:

```http
### Report as CSV
@header.Accept = ~(?i).*text/csv.*
GET /report
Content-Type: text/csv

id,total
1,9.99

### Report as JSON
GET /report
Content-Type: application/json

[{"id": 1, "total": 9.99}]
```

## Case-insensitive values

`(?i)` works in every `~regex` matcher, including query parameters and body
fields. `@query.sort = ~(?i)asc|desc` accepts `sort=ASC` and `sort=desc` but
not `sort=up`.

## Different responses per test

To give each test its own answer from one mock, have the test send a header
that names the scenario, and put the default route last:

```http
### Empty cart
@header.X-Test = empty-cart
GET /cart
Content-Type: application/json

{"items": []}

### [503] Cart service down
@header.X-Test = cart-down
GET /cart
Content-Type: application/json

{"error": "unavailable"}

### Cart
GET /cart
Content-Type: application/json

{"items": [{"sku": "A1", "qty": 2}]}
```

`curl -H 'X-Test: cart-down' localhost:8080/cart` gets the `503`; a request
without the header gets the full cart. In Go tests, [`mocktest`](testing.md#using-mock-in-go-tests)
starts a fresh server per test instead.

## Waiting until mock is ready

`mock` binds every port and loads every route before it answers on the admin
port, so a successful `GET /state` means it is ready. In a CI script:

```sh
mock -i 1s api.http &
until curl -fs -o /dev/null http://127.0.0.1:8081/state; do sleep 0.2; done
```

The Docker image is based on Alpine, which has `wget` but not `curl`. In
Docker Compose:

```yaml
services:
  api:
    image: mock
    command: ["-b", "0.0.0.0", "-i", "1s", "/mock/api.http"]
    volumes:
      - ./mocks:/mock:ro
    ports: ["8080:8080", "8081:8081"]
    healthcheck:
      test: ["CMD", "wget", "-q", "-O", "/dev/null", "http://127.0.0.1:8081/state"]
      interval: 2s
      retries: 10
  app:
    depends_on:
      api:
        condition: service_healthy
```
