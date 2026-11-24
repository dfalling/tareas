package db

// TestLockID is the advisory-lock key that serializes integration tests which
// truncate or mutate shared tables. `go test ./...` runs each package's test
// binary in parallel against the same TEST_DATABASE_URL, so every
// DB-mutating integration test holds this lock (on its own pooled
// connection) for the whole run: pg_advisory_lock is what keeps, say, the
// web auth-flow test from truncating the users table underneath the todos
// service test.
const TestLockID int64 = 0x74617464 // "tatd"
