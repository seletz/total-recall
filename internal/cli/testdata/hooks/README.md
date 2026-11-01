# Hook payload fixtures

Synthetic stdin payloads of the Claude Code hooks total-recall installs
(`SessionStart`, `Stop`, `SubagentStop`, `SessionEnd`). `{{PROJECTS}}` is
replaced with the test's projects directory.

Field names follow the official hooks reference,
<https://code.claude.com/docs/en/hooks> ("Common input fields" and the input
sections of each event), checked on 2026-11-01 against the page as published
then (it mentions Claude Code versions up to v2.1.290; the local Claude Code
was 2.1.316). The page has no version or date of its own.

total-recall reads only `session_id`, `transcript_path`, `cwd`,
`hook_event_name` and, for `SubagentStop`, `agent_transcript_path` (the
subagent's own transcript below `<session>/subagents/`; `transcript_path` is
the main session's). All other fields (`last_assistant_message`,
`background_tasks`, `session_crons`, `effort`, `agent_id`, `agent_type`, ...)
are present to make sure unknown fields are ignored. `future_field` simulates
a field added later.
