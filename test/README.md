# Tests

Split by what a test needs to run, not by what it covers. Build tags keep the
layers apart, so `go test ./...` never needs a database.

| Layer | Lives in | Needs | Run with |
|-------|----------|-------|----------|
| unit | `internal/**/*_test.go` | nothing | `make test-unit` |
| integration | `test/integration/` | postgres, redis | `make test-integration` |
| feature | `test/feature/` | postgres, redis, mail server, the whole app | `make test-feature` |

`make test` runs all three. `make test-verbose` adds `-v`.
Pass anything else through with `ARGS`, for example
`make test-feature ARGS="-run TestPipeline -v"`.

## unit

Pure functions, no I/O, milliseconds. They sit beside the code they cover,
which is the Go convention and keeps them impossible to miss.

Covers the filter matcher (every field and operator, match modes, case
sensitivity, invalid regex), the AES-GCM cipher (round trip, tampering, wrong
key), and the small pieces of logic on entities and models.

## integration

Repositories, seeders and gateways against **real** Postgres and Redis — no
mocks, because the things worth testing here are SQL behaviours: the dedupe
index, `SKIP LOCKED` claims, the roles pivot, seeder idempotency.

Tagged `//go:build integration`.

## feature

The whole application over HTTP: a request goes through the real router,
middleware, usecases and database, and the assertions are made on the JSON a
SPA would receive. Nothing is stubbed.

Tagged `//go:build feature`. These are the tests that catch a route mounted in
the wrong group, a missing ownership check, or a response that stopped
including a field the front end needs.

`pipeline_test.go` is the widest: it delivers real mail over SMTP, syncs it
over IMAP, and follows the match through scheduling, dispatch and delivery.

## Writing one

`test/support` builds the app once per package and gives you `Register`,
`Login`, `Get/Post/Patch/Put/Delete`, and `Reset` to truncate between tests.

```go
func TestSomething(t *testing.T) {
    h := support.New(t)
    h.Reset(t)

    account := h.Register(t, "someone@example.com", "secret123")

    response := h.Get(t, "/api/watchers", account.Token)
    require.Equal(t, http.StatusOK, response.Status)
}
```

Call `h.Reset(t)` at the start of each top-level test: it truncates every table
that hangs off a user and clears cached auth, leaving the seeded reference
tables (`roles`, `mail_providers`) alone.
