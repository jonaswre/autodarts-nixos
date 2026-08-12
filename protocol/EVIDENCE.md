# Board Manager 1.0.7 evidence

This corpus was captured from a real three-camera Linux appliance on
2026-08-12. No camera images are included. Identifiers, addresses, credentials,
USB topology, and device paths are redacted before files are written.

## Play game-state journey

`testdata/play/random-checkout-64.jsonl` is a sanitized 184-event real match
from target 64 through checkout. The initial match snapshot came from
`GET /gs/v0/matches/{id}/state`; subsequent changes arrived on the
`autodarts.matches` Play WebSocket channel. `state.checkoutGuide` changed after
detected hits, misses, and a bust. The final D2 produced `game_shot`, score
zero, `gameFinished: true`, and winner index 0.

The journey also records board Throw and takeout transitions. A reproduced
zero-dart false takeout was recovered using Board Manager's supported
`POST /api/reset` action without restarting the appliance.

## Captured journeys

| Capture | Evidence |
| --- | --- |
| `board-1.0.7-stopped` | Thirteen HTTP snapshots while detection is stopped |
| `board-1.0.7-throw-session` | Starting, started, manual reset, two detected throws, camera/stat/motion traffic |
| `board-1.0.7-three-darts` | HTTP snapshot with three complete throw objects |
| `board-1.0.7-takeout-session` | Takeout started/finished, stopping/stopped, camera/stat/motion traffic |
| `board-1.0.7-calibration-session` | Start plus successful automatic calibration lifecycle |
| `board-1.0.7-post-calibration` | Thirteen HTTP snapshots after calibration |

The first physical visit happened while cameras were stabilizing and was not
scored. Removing those darts produced a genuine `Takeout started` with zero
throws; a manual reset returned the detector to `Throw`. This is a valuable
edge case and is retained in the raw event stream.

The recorded scored visit was `S2`, `S18`, `S14`. A throw contains normalized
`coords.x` and `coords.y`, plus `segment.name`, `segment.number`,
`segment.bed`, and `segment.multiplier`. Every `state` update contains the
complete visit-so-far rather than only the newest dart.

Takeout started retains the three throws. Takeout finished has `numThrows: 0`
and omits `throws`. Start and stop use two transitions each:
`Starting -> Started` and `Stopping -> Stopped`.

## WebSocket envelope

`ws://BOARD:3180/api/events` sends JSON objects shaped as:

```json
{"type":"state","data":{}}
```

The shipped Board Manager client subscribes to `state`, `motion_state`,
`stats`, `cam_state`, `cam_stats`, and `calibration_state`. Five types are
present in this corpus. A real `POST /api/config/calibration/auto` run emitted
`state` events with `Calibration started` and `Calibration finished`, but no
`calibration_state` envelope. Inspection of the shipped UI associates
`calibration_state` with the separate interactive checkerboard/lens-distortion
workflow, whose payload starts with `id`, `stage`, `images`, `minImages`,
`coverage`, and `minCoverage`.

The automatic calibration remained valid for all three cameras. Distortion
error stayed approximately equal for cameras 0 and 1 and improved from about
0.65 to 0.57 for camera 2. The post-calibration HTTP fixtures contain the
resulting geometry.

The socket sends changes, not an initial snapshot. A client must fetch HTTP
state first and then merge WebSocket updates. `cam_stats`, `stats`, and
especially `motion_state` are high-frequency streams; consumers need bounded
buffers and must not assume every update can be persisted.

## Compatibility guidance

- Treat `event`, `status`, segment bed names, and unknown JSON fields as open
  sets so a newer Board Manager remains decodable.
- Treat `throws` as optional. Its absence is different from a populated visit.
- Use `json.RawMessage` at the event-envelope boundary before decoding by type.
- Coordinates are normalized floating-point board coordinates and may be
  negative.
- `cam_stats` updates one camera at a time and includes its numeric `id`.
- Do not expose `/api/config` from a proxy: it contains the board API key before
  corpus redaction.
- Mutating endpoints in `actions.json` were extracted from the exact shipped
  Board Manager client; only start, reset, and stop were exercised here.
