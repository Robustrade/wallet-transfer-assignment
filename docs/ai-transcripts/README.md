# AI Session Transcripts

Raw Claude Code session logs (JSONL — one JSON object per line: user/assistant messages, tool
calls and their results, thinking blocks) for the three phases this submission's AI usage
disclosure describes, in chronological order:

1. **`01-setup.jsonl`** — repository/project setup: making the project ADLC/AI-native-development
   friendly, initial scaffolding.
2. **`02-design-and-adr-review.jsonl`** — the design phase and the independent, adversarial
   architecture review that found and fixed the contradictions documented throughout
   `docs/decisions/adrs/` (e.g. the UUIDv7 pre-generation requirement, the deferrable idempotency
   FK), ending with `HANDOVER.md` written as the implementation kickoff briefing.
3. **`03-implementation-and-tests.jsonl`** — the implementation session itself: everything from
   `HANDOVER.md`'s readiness check through the full implementation, test suite, the two real
   concurrency bugs found and fixed via real-Postgres testing, the Docker/demo tooling, and this
   PR's preparation.

These are the tool's own internal format, not a curated chat transcript — a JSONL viewer or a
plain text editor will page through them; each line's `"message"` field (for `type: "user"` /
`type: "assistant"` entries) carries the actual conversational content.
