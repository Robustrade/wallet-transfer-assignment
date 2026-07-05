# AI Usage Disclosure

> This file is a template — fill in the bracketed parts honestly before you
> submit. The assignment explicitly asks for this, and asks you to be able
> to explain the PR in discussion afterward, so make sure you actually
> understand every piece before you fill this in and submit.

## 1. Tool used

Claude (Anthropic), used via [claude.ai chat interface / Claude Code — pick
whichever you actually used].

## 2. How I generally use the tool for my work

I use Claude mainly for scaffolding — getting a working first draft of a layered architecture, database schema, or test suite quickly — then reading through it critically, adjusting business logic decisions, and verifying it actually compiles and passes tests before treating it as done. I also use it for things like interview prep, resume review, and explaining unfamiliar concepts when I'm ramping up on something new. I don't use it to submit code I haven't personally understood or tested.

## 3. How it was used on this assignment

I asked Claude to implement the wallet transfer service end-to-end from the
assignment brief: domain models, the repository interface + Postgres and
in-memory implementations, the transfer service (idempotency, validation,
locking order, insufficient-funds handling), the HTTP handler, the DB
schema, and the test suite (unit, HTTP, and concurrency tests).

Before submitting, I:
- [ ] Read every file and can explain what each layer does and why
- [ ] Ran `go build ./...` and `go test ./... -race` locally and fixed
      anything that didn't compile or pass
- [ ] Can explain the idempotency claim/replay flow without looking at the
      code
- [ ] Can explain why wallet locks are acquired in ID-sorted order
- [ ] Can explain why insufficient-funds and unknown-wallet are handled
      differently (see README "Assumptions & tradeoffs")
- [ ] Would change/did change: [note anything you modified, disagreed with,
      or simplified further]

