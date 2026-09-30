# Debugging

`JIG_DEBUG=1 jig` (or `JIG_DEBUG=1 jig run ...`; any non-empty value
works) writes a debug log to `jig-debug.log` in the working directory. The
file is **truncated on each start**, so two jigs in the same directory
overwrite each other's log: reproduce the issue, quit, then copy the file
before relaunching. Without `JIG_DEBUG` nothing is written. Add
`jig-debug.log` to your `.gitignore`.

## What's in it

Each line is `key=value` text with a `cat=` category:

- `app`: startup (version, working directory, PID, default model).
- `run`, `step`, `retry`: each agent run's start and outcome (`done`,
  `max_steps`, `failed`, `cancelled`), every model step with its timings
  and token counts, and every failed attempt with whether jig will retry.
- `http`: each provider request: status, time to first byte, total
  duration, provider request ID, and transport errors or dropped streams.
- `task`: subagents spawned, finished, or rejected.
- `tool`: each tool call's duration, output size, and whether it failed
  or was blocked.

Lines from a run carry `root=`, `session=`, `depth=`, and `agent=`, HTTP
lines included, so `grep session=<id> jig-debug.log` shows one subagent's
whole timeline, and `grep cat=http jig-debug.log` slices to provider
traffic.

## What's not in it

The log never contains API keys or other headers (the one exception is the
provider's request-ID header value, logged as `req_id=`), prompts, tool
input or output, or streamed text. The one body it records is the first
4 KB of a non-2xx HTTP response. `err=` carries the provider's error
message, which may include the request URL and the error body.

Every string value is sanitized to one line, has any URL userinfo
(`https://user:pass@host`) replaced with `https://…@host`, and is capped
at 4 KB. If `jig-debug.log` already exists and is not a regular file (a
symlink, say), jig refuses it with a warning and logs nothing.
