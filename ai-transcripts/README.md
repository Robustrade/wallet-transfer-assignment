# AI Usage Transcripts

This folder contains a complete record of AI tool usage for this submission, as required by the assignment.

## Tool used

**Claude Code** (Anthropic), model: Claude Sonnet 4.6.

This is Anthropic's CLI coding agent, run from the terminal. It has access to the local filesystem and can read, write, and edit files; run shell commands; and execute tests directly. All actions happen in the user's working directory and are visible in real time.

## How I generally use this tool

I use Claude Code as a pair-programming agent rather than a code generator. My typical workflow:

1. I think through the design first — schema, concurrency strategy, idempotency model, transaction boundaries — and write a structured kickoff prompt that locks in the non-obvious decisions.
2. Claude Code implements layer-by-layer, pausing for review at each step (the kickoff prompt explicitly asks it to wait for "continue" between phases).
3. I review every diff before accepting it, run tests locally, and push back when something looks off. Re-prompts in this session include corrections on Go pointer receiver style, idempotency error semantics, and method extraction for cognitive complexity.
4. For *design* discussions (which database isolation level, framework choice, schema tradeoffs) I use the claude.ai web app with a more capable model (Opus 4.7) before bringing the locked-in decision into Claude Code. That separation keeps the implementation session focused on writing code, not re-litigating design.

For this assignment, the kickoff prompt (the first message in the transcript) was the result of a separate design discussion in the web app and is the primary "thinking" artifact. The Claude Code session below is the implementation execution against that locked design.

## Files in this folder

| File | Description |
|------|-------------|
| `claude-code-session.md` | Human-readable transcript. **Start here.** |
| `claude-code-session.jsonl` | Raw unmodified export from Claude Code (`/export` command). |
| `convert.py` | Script that produces the markdown from the JSONL. Run `python3 convert.py claude-code-session.jsonl > claude-code-session.md` to regenerate. |
| `metadata.json` | Session metadata from the export. |

## What was AI-generated vs human-authored

- **Designed by me**: schema (constraints, indexes, ENUMs), concurrency strategy (`SELECT FOR UPDATE` with sorted UUID lock ordering), idempotency model (dedicated table + request_hash, same-transaction guarantee), 14-step transfer flow, test plan, API contract.
- **Implemented with Claude Code**: Go source files for the locked design, test scaffolding, Makefile, docker-compose, README structure.
- **Reviewed and revised by me**: every file. Specific corrections during the session include `errors.Is` vs `==` for pgx errors, removing semantically wrong error returns, refactoring repositories to pointer receivers, extracting `handleInsufficientFunds` and `executeSuccessfulTransfer` from a single oversized service method, and adding graceful shutdown to `main.go`.

Approximate split: design and structural decisions are mine; mechanical code production is Claude's; correctness verification (lint, tests, race detector, code review) is mine.