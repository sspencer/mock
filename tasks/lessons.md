# Lessons

- Keep dynamic placeholder generation simple: when the existing `generatedValue` switch is sufficient, update that switch directly instead of introducing a reflective/template adapter.
- Hot-reload: watch parent directories (not files) for editor atomic saves; debounce and retry once on parse errors from half-written files.
- SSE UIs need stable event IDs + `Last-Event-ID` and a server clear endpoint; client-only clear is not enough after reconnect.
- Prefer extracting `run(args, …) error` so CLI wiring is testable without starting a real process forever.
