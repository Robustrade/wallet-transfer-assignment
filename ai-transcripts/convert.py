#!/usr/bin/env python3
"""
Convert a Claude Code session JSONL export into a human-readable markdown transcript.

Usage:
    python3 convert.py claude-code-session.jsonl > claude-code-session.md
"""

import json
import sys
from datetime import datetime


def fmt_ts(iso: str) -> str:
    """Render ISO timestamp as YYYY-MM-DD HH:MM:SS UTC."""
    try:
        dt = datetime.fromisoformat(iso.replace("Z", "+00:00"))
        return dt.strftime("%Y-%m-%d %H:%M:%S UTC")
    except Exception:
        return iso


def summarize_tool_use(block: dict) -> str:
    """Render a tool_use block as a single readable line."""
    name = block.get("name", "unknown")
    inp = block.get("input", {}) or {}

    if name in ("Bash", "bash_tool"):
        cmd = inp.get("command", "").strip()
        # Truncate very long commands
        if len(cmd) > 200:
            cmd = cmd[:200] + " …"
        return f"**[bash]** `{cmd}`"

    if name in ("Read", "view"):
        path = inp.get("file_path") or inp.get("path", "?")
        return f"**[read]** `{path}`"

    if name in ("Write", "create_file"):
        path = inp.get("file_path") or inp.get("path", "?")
        text = inp.get("content") or inp.get("file_text") or ""
        lines = text.count("\n") + 1 if text else 0
        return f"**[write]** `{path}` ({lines} lines)"

    if name in ("Edit", "str_replace"):
        path = inp.get("file_path") or inp.get("path", "?")
        return f"**[edit]** `{path}`"

    if name in ("Grep", "Glob"):
        pattern = inp.get("pattern", "?")
        return f"**[{name.lower()}]** `{pattern}`"

    if name == "TodoWrite":
        todos = inp.get("todos", [])
        return f"**[todo]** updated todo list ({len(todos)} items)"

    # Generic fallback
    keys = ", ".join(inp.keys())
    return f"**[{name}]** ({keys})"


def is_tool_result_content(content) -> bool:
    """Detect user messages that are actually tool result payloads."""
    if isinstance(content, list):
        return any(
            isinstance(b, dict) and b.get("type") == "tool_result"
            for b in content
        )
    return False


def render_user(evt: dict) -> str:
    msg = evt.get("message", {})
    content = msg.get("content", "")
    ts = fmt_ts(evt.get("timestamp", ""))

    # Skip tool_result echo-backs — they're noise, not real user input
    if is_tool_result_content(content):
        return ""

    # Plain string content
    if isinstance(content, str):
        text = content.strip()
        if not text:
            return ""
        return f"\n\n## 👤 User — {ts}\n\n{text}\n"

    # Structured content (rare for user messages)
    if isinstance(content, list):
        parts = []
        for block in content:
            if isinstance(block, dict) and block.get("type") == "text":
                parts.append(block.get("text", "").strip())
        text = "\n\n".join(p for p in parts if p)
        if not text:
            return ""
        return f"\n\n## 👤 User — {ts}\n\n{text}\n"

    return ""


def render_assistant(evt: dict) -> str:
    msg = evt.get("message", {})
    content = msg.get("content", [])
    ts = fmt_ts(evt.get("timestamp", ""))

    text_parts = []
    tool_calls = []

    if isinstance(content, list):
        for block in content:
            if not isinstance(block, dict):
                continue
            btype = block.get("type")
            if btype == "text":
                t = block.get("text", "").strip()
                if t:
                    text_parts.append(t)
            elif btype == "tool_use":
                tool_calls.append(summarize_tool_use(block))
            # 'thinking' blocks are stripped — internal reasoning, not for reviewers

    if not text_parts and not tool_calls:
        return ""

    sections = [f"\n\n## 🤖 Claude — {ts}\n"]
    if text_parts:
        sections.append("\n\n".join(text_parts))
    if tool_calls:
        sections.append("\n_Tool calls:_\n" + "\n".join(f"- {tc}" for tc in tool_calls))

    return "\n".join(sections) + "\n"


def main():
    if len(sys.argv) != 2:
        print("Usage: convert.py <session.jsonl>", file=sys.stderr)
        sys.exit(1)

    out = []
    out.append("# Claude Code Session Transcript\n")
    out.append(
        "_This transcript was generated from the raw Claude Code session export "
        "(`claude-code-session.jsonl`) using `convert.py` in this directory. "
        "Thinking blocks (Claude's internal reasoning) are omitted for readability. "
        "Tool calls (file reads, edits, bash commands) are summarized as single lines. "
        "The full unmodified export is preserved alongside this file._\n"
    )

    with open(sys.argv[1]) as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            try:
                evt = json.loads(line)
            except json.JSONDecodeError:
                continue

            t = evt.get("type")
            if t == "user":
                out.append(render_user(evt))
            elif t == "assistant":
                out.append(render_assistant(evt))
            # All other event types (attachment, system, ai-title, last-prompt,
            # queue-operation) are intentionally skipped — they're internals,
            # not conversation.

    print("".join(out))


if __name__ == "__main__":
    main()