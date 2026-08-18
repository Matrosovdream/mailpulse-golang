# Seeds

One JSON file per table, named after the seeder that reads it
(`users.json` is read by the `users` seeder).

Run them with `make seed`, or one at a time with `make seed-only name=users`.

- Seeders are **idempotent**: running them twice changes nothing the second time.
- An existing user is never overwritten — only missing roles are added, so a
  re-seed cannot reset a password someone has changed.
- Deleting a file opts that table out; the seeder reports it as skipped.

Adding a table: write a `Seeder` in `internal/seeder/`, register it in
`Default()`, and drop a JSON file here with the same name.

**Passwords in `users.json` are plaintext and hashed on insert.** These are
bootstrap accounts for development. Never put a real credential in this folder.
