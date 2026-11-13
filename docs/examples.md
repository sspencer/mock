[Documentation](README.md) › Example gallery

# Example gallery

[`examples/gallery`](../examples/gallery/) holds request files for APIs that
come up again and again. Each one is a working mock to start from: copy it,
rename the routes, and change the data. CI loads every file with
`mock validate -strict` and sends each route the requests shown here, so the
examples keep working as `mock` changes.

Serve one file, or the whole gallery at once (the paths don't overlap):

```sh
mock examples/gallery/payments.http
mock -d examples/gallery
```

| File | What it mocks | Shows |
|------|---------------|-------|
| [`oauth.http`](#oauth-token-endpoint) | An OAuth 2.0 token endpoint | form matchers, regex captures, signed JWTs, `@default.header` |
| [`pagination.http`](#paginated-list) | A product catalog | fixtures, `{{#each}}` paging, lookup by id, `@require`, `{{#if}}` |
| [`uploads.http`](#file-uploads) | File uploads | multipart matching, size and type checks, `\| keep` |
| [`webhooks.http`](#webhook-sender) | A service that calls webhooks | callback sequences, retries, HMAC signatures |
| [`payments.http`](#payment-api-with-failures) | A card payment API | test card numbers, idempotency keys, rate limits per key, random failures |

## OAuth token endpoint

[`oauth.http`](../examples/gallery/oauth.http) answers the token endpoint for
the `client_credentials`, `password` and `refresh_token` grants, plus token
introspection and a `/userinfo` endpoint that needs a bearer token.

```sh
curl -s localhost:8080/oauth/token -d grant_type=client_credentials \
  -d client_id=demo-app -d client_secret=demo-secret
curl -s localhost:8080/oauth/token -d grant_type=password -d username=ada -d password=x
curl -s localhost:8080/userinfo -H 'Authorization: Bearer eyJ...'
```

- The `grant_type` [form matcher](matching.md#body-matching) picks the route;
  a wrong `client_secret` gets `401 invalid_client`, an unknown grant
  `400 unsupported_grant_type`.
- Access tokens are real [JWTs](placeholders.md#signed-tokens) signed with
  `@jwt.secret` (from `$OAUTH_SECRET`, else a dev secret), with `sub`, `scope`
  and `exp` claims, so client libraries can decode them and refresh on expiry.
- The password grant captures the username with a named regex group and puts
  it in the refresh token, which the refresh grant captures back into `sub`.
- `@default.header` sets `Content-Type` and `Cache-Control: no-store` on every
  response once, as OAuth servers do.

## Paginated list

[`pagination.http`](../examples/gallery/pagination.http) serves a 25-product
catalog from [`data/products.json`](../examples/gallery/data/products.json).

```sh
curl -s 'localhost:8080/products?offset=10&limit=5'
curl -s localhost:8080/products/7
curl -s localhost:8080/products/99                 # 404
curl -s localhost:8080/categories/storage/products
```

- `{{#each $fixture.products | offset $query.offset | limit $query.limit}}`
  [pages through the fixture](placeholders.md#one-copy-per-request-item), and
  `{{$fixture.products | length}}` gives the total.
- With `?limit=`, an [`{{#if}}` block](placeholders.md#conditional-blocks) adds
  the limit and a `next` link, computed with `| add $query.limit`.
- `/products/:id` [looks the record up](placeholders.md#looking-up-a-record)
  with `[id=$path.id]`; `@require` sends unknown ids to the `404` route.
- The category route filters with `{{#if $item.category == $path.category}}`
  inside `{{#each}}`; skipped items leave no stray commas.
- Edit the JSON file and the next request sees the change, or edit it in the
  [admin UI](admin-ui.md#editing-fixtures).

## File uploads

[`uploads.http`](../examples/gallery/uploads.http) accepts images posted as
`multipart/form-data` and raw uploads with `PUT`.

```sh
curl -s localhost:8080/uploads -F file=@photo.png -F description=Holiday
curl -s localhost:8080/uploads -F file=@report.pdf             # 415
curl -s -X PUT localhost:8080/uploads/logo.png -H 'Content-Type: image/png' --data-binary @logo.png
```

- `@form.file` matches a file part by its file name, so
  `~(?i)(?P<filename>.+\.(png|jpe?g|gif|webp))` accepts images and captures
  the name; any other file gets `415`, and a form without a `file` part `400`.
- The size limit checks `@header.Content-Length` with a regex, before the
  body matters, so it works for uploads larger than the 1 MiB `mock` reads
  for matching (`--max-body`).
- `{{$ulid | keep}}` uses one generated id in the `Location` header and the
  body.

## Webhook sender

[`webhooks.http`](../examples/gallery/webhooks.http) acts like a service that
reports progress to your webhook: an order is answered at once, then
`order.created` and, two seconds later, `order.shipped` are posted to the
`webhook_url` in the request.

```sh
curl -s localhost:8080/orders -H 'Content-Type: application/json' \
  -d '{"id": "ord_1", "webhook_url": "http://localhost:9000/hooks"}'
```

- `@callback` and `@callback.1` form a
  [callback sequence](responses.md#callback-sequences); each waits for the one
  before it.
- `@callback.retries = 3` sends a call again when your receiver is down or
  answers `5xx`, with growing waits, so you can test that the receiver copes
  with duplicates.
- `X-Signature: sha256=...` is an HMAC-SHA256 of the order id made with the
  [`hmac` filter](placeholders.md#filters), for practising signature checks.
- The admin UI logs each call (marked **callback**, or **retry**) with the
  response your receiver gave.

## Payment API with failures

[`payments.http`](../examples/gallery/payments.http) is a card payment API
that fails the way real ones do. Test card numbers choose the outcome:

| Card number | Result |
|-------------|--------|
| `4242424242424242` | `201`, the charge succeeds |
| `4000000000000002` | `402 card_declined` |
| `4000000000009995` | `402 insufficient_funds` |
| `4000000000003220` | `200`, `requires_action` with a 3-D Secure redirect |
| `4000000000000119` | `500 processing_error` |

```sh
curl -s localhost:8080/v1/payments -H 'Authorization: Bearer sk_test_1' \
  -H 'Idempotency-Key: k1' -H 'Content-Type: application/json' \
  -d '{"amount": 2500, "currency": "usd", "card": {"number": "4000000000000002"}}'
curl -s localhost:8080/v1/payments/pay_1001 -H 'Authorization: Bearer sk_test_1'
```

- Requests without a `Bearer sk_test_...` key get `401`; charges without an
  `Idempotency-Key` header get `400`, and a non-positive amount `400`.
- Each API key may create 20 charges a second. After that it gets `429` with
  `Retry-After`, and every charge carries `X-RateLimit-*` headers
  ([rate limits per key](responses.md#rate-limits)).
- Send `X-Mock-Chaos: on` and one charge in ten fails with `503`
  ([`@match.chance`](matching.md#occasional-responses)), to test retries.
- Payments and refunds read [`data/payments.json`](../examples/gallery/data/payments.json);
  a refund without an `amount` refunds the whole payment
  (`| default $fixture.payments[id=$path.id].amount`).
- For latency on top, start it with a [network profile](responses.md#latency-and-faults-for-every-route):
  `mock -P flaky examples/gallery/payments.http`.
