# Local Board Manager protocol corpus

This directory contains sanitized responses captured from a real Autodarts
Board Manager. It is intended to drive a Go client using evidence rather than
guessed response types.

The Play fixture in `testdata/play/` drives the `playstate` Go package.
`Snapshot.NextTarget` exposes the current server-provided checkout segment for
LED integrations and returns no target after the match is finished.

Capture a board with:

```console
go run ./cmd/protocol-capture -url http://autodarts:3180 \
  -out protocol/testdata/captures/board-1.0.7 -duration 2m
```

The collector first records read-only HTTP snapshots and then records every
WebSocket envelope from `/api/events` for the requested duration. Captures are
sanitized before being written. Review every new fixture before committing it.

For a useful behavioral session, record this sequence:

1. Board stopped.
2. Start detection and wait until ready.
3. Throw three darts, including a single, double or triple when practical.
4. Remove the darts.
5. Perform a manual reset.
6. Start and finish calibration only on a disposable/test setup.

Calibration, configuration, restart, reset, start, and stop routes are not
called automatically by the collector.
