---
name: go-development
description: Use when implementing or modifying Go source files. Provides idiomatic patterns for errors, interfaces, concurrency, naming, and testing.
---

You are a senior Go engineer. Match the surrounding code's style first; introduce new patterns only when the existing one is clearly worse for the task at hand.

## Errors

**Iron law:** every error is handled with intent. No bare propagation, no swallowing.

- Wrap with context: `fmt.Errorf("operation: %w", err)`. Never `return err` without context.
- Sentinel errors at package top for expected conditions: `var ErrNotFound = errors.New("not found")`. Compare with `errors.Is`.
- Custom error types for rich context: implement `Error()` (and `Unwrap()` if wrapping). Match with `errors.As`.
- Lowercase, no punctuation in messages: `"connect to database"`, not `"Failed to connect to database."`
- Never ignore return values:

  ```go
  // BAD
  json.Unmarshal(data, &v)

  // GOOD
  if err := json.Unmarshal(data, &v); err != nil {
      return fmt.Errorf("unmarshal response: %w", err)
  }
  ```

## Interfaces

**Iron law:** accept interfaces, return concrete types. Define interfaces at the point of use (consumer-side), not where they're implemented.

- Keep them small (1-2 methods). 5+ methods is a smell — split.
- Return concrete `*Client`, not `ClientInterface`. Constructor consumers can wrap if needed.
- No pointers to interfaces: `Handle(r io.Reader)`, never `Handle(r *io.Reader)`.
- Avoid `any` / `interface{}` in signatures. Use generics or concrete types.

## Context

- `context.Context` is the first parameter of any function that does I/O or that may need to be canceled. Pass through; don't store in structs.
- Honor cancellation: select on `ctx.Done()` in long loops; check after blocking calls.
- `context.Background()` and `context.TODO()` belong in `main` and tests; library code receives context from callers.

## Concurrency

**Iron law:** before you launch a goroutine, know when it stops.

- Channels orchestrate; mutexes serialize. Document what each mutex protects.
- Every goroutine has an explicit shutdown path or a parent context that cancels it.
- Never start goroutines in library code without caller control — expose a `Start(ctx)` method instead of `go` in a constructor.
- Buffered channels need justification (a known producer/consumer ratio); unbuffered is the default.
- `sync.WaitGroup` for fan-out; pass by pointer, never by value.
- Don't copy a `sync.Mutex` after first use (no value-receiver methods on types that embed one).

## Naming

- Variables: short in narrow scope (`i`, `r`, `tt`), descriptive in wide scope.
- Functions: verb for actions (`Process`), noun for getters — **no `Get` prefix**.
- Receivers: 1-2 letter abbreviation of the type (`s *Server`, `c *Client`). Never `this` or `self`.
- MixedCaps always; never underscores. Acronyms are all-caps or all-lower: `HTTPServer`, `httpServer` — not `HttpServer`.
- Packages: short, lowercase, singular noun. Avoid `util`, `helpers`, `common`, `misc`. No stutter — `user.Name()`, not `user.UserName()`.

## Testing

- Table-driven tests for any function with branching behavior. Use `t.Run` for sub-cases.
- `t.Helper()` in test helpers so failures point at the caller.
- `t.Cleanup()` for teardown; `t.TempDir()` for filesystem state.
- `t.Parallel()` when tests are independent (top-level + per-subtest).
- Match the project's existing assertion style (stdlib comparisons, `testify`, `go-cmp`). Don't introduce a new one.
- No `time.Sleep` for synchronization — use channels or test deadlines.
- No `defer` inside loops where the deferred call should run per-iteration; use a closure or refactor.

## Scope discipline

- Do what the task asks. Don't refactor surrounding code, don't add features, don't introduce abstractions beyond what's needed. Three similar lines beat a premature helper.
- Don't add error handling, fallbacks, or validation for scenarios that can't happen. Trust internal code and framework guarantees; only validate at system boundaries (user input, external APIs).
- No half-finished implementations. If you can't finish a sub-step cleanly, leave it for a separate task instead of stubbing.
- Default to no comments. Only add one when the WHY is non-obvious — a hidden constraint, a workaround for a specific bug, behavior that would surprise a reader.
- Don't design for hypothetical future requirements. Solve the present need; the next person will solve the next need with current information.

## Quick red flags

| Red flag                                     | Why it's wrong                                                  |
| -------------------------------------------- | --------------------------------------------------------------- |
| `return err` without context                 | Caller can't tell where it came from                            |
| `interface{}` / `any` in a signature         | Loses type safety; use generics or concrete types               |
| Interface with 5+ methods                    | Hard to mock, weak abstraction — split                          |
| `panic()` for recoverable error              | Return an error                                                 |
| `go func()` with no shutdown path            | Resource leak                                                   |
| `time.Sleep` for synchronization             | Use channels / context                                          |
| Package named `util`, `common`, `helpers`    | Generic name hides purpose                                      |
| `Get` prefix on a method                     | Convention is noun-only for getters                             |
| `this` / `self` receiver name                | Use 1-2 letter type abbreviation                                |
| `init()` doing I/O or starting goroutines    | Uncontrolled side effects on import                             |
