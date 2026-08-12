# Go client

Package `autodarts` presents Board Manager detection and Play match state as
one client. It is based on the real protocol corpus in `protocol/testdata`.

```go
client, err := autodarts.New("http://autodarts:3180")
if err != nil { log.Fatal(err) }

board, err := client.BoardState(ctx)
events, failures, err := client.Events(ctx)
for event := range events {
    if _, err := client.ApplyBoardEvent(event); err != nil { log.Print(err) }
    if target, ok := client.Snapshot().NextTarget(); ok {
        leds.Highlight(target.Number, target.Bed)
    }
}
```

The appliance's browser bridge supplies sanitized Play records to
`ApplyPlayEvent`. This avoids a second login or copied cloud credentials. The
same `Snapshot` then contains board status, detected throws, game variant,
remaining score, round, checkout guide, last throw, and completion state.

Read operations cover state, host, version, cameras, devices, configuration,
calibration, and distortion. Supported control methods cover start, stop,
visit reset, restart, full/per-camera auto-calibration, and configuration
patching. Raw JSON is retained for configuration structures that may vary by
Board Manager release.
