## Decisions

- **File capture, not pipes.** os/exec with a non-file writer creates a pipe and waits for EOF on it; any descendant that inherits it (a relaunched dev server) delays Wait. A temp file has no EOF coupling. Output is tailed to 64 KiB.
- **Default 180s.** The Sketch restart script alone can spend ~7s on credentials plus up to 10s per lsof port reclaim plus a 5s port wait, exceeding 30s with margin. `timeout_seconds` already existed; only the default moves.
- **Process-group kill on timeout is unchanged**, so a timed-out script and anything it spawned in its group are killed.
