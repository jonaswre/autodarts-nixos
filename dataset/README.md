# Autodarts multi-camera dataset recorder

`autodarts-dataset` collects training evidence from normal play without taking
control of the cameras. It listens to Board Manager events and continuously
buffers Board Manager's three MJPEG streams. It does not open `/dev/video*`,
change detection settings, or upload data.

Run it on the appliance or another machine that can reach Board Manager:

```console
go run ./cmd/autodarts-dataset \
  -url http://127.0.0.1:3180 \
  -output ./autodarts-dataset \
  -quota-gb 20 \
  -listen 0.0.0.0:8090
```

Open `http://DEVICE-IP:8090` while the recorder is running. The dashboard
updates every two seconds and shows training-ready sample count, excluded
historical evidence, quota usage, physical dart count, world transitions,
exact teacher coordinates, and three-camera before/after previews.
The preview loads only the latest 24 samples so it remains responsive as the
dataset grows into the thousands. Its optional board-feature overlay draws the
per-camera board ROI, double and treble contours, bull, and calibration points
captured from Autodarts.
For teacher-labeled states it also projects every dart coordinate through the
four-point board-to-camera homography and draws a numbered marker at the impact
point. Accepted-dart previews additionally draw Board Manager's fitted
tip-to-flight `imageLine` in orange and mark its detected tip. Before images
show `world_before`; after images show `world_after` and the fitted line.
Accepted darts also get an interactive, orbitable 3D reconstruction. The
recorder combines the board-plane homography with each camera's intrinsic
matrix to recover its board-relative pose. Every 2D `imageLine` becomes a plane
through its camera; a least-squares plane intersection gives the dart shaft
direction. The exact teacher coordinate anchors the tip at
`z = 0`. The preview reports millimetres, contributing camera-line count, and
angular fit residual. Its 140 mm dart length is explicitly a visualization
length; the direction and tip are the reconstructed quantities.
The default renderer uses regulation scoring radii, alternating sisal fields,
red/green double and treble beds, spider wires, sector numbers, a board cabinet
and layered steel-tip dart geometry (point, barrel, shaft, and flights). Users
can orbit or zoom the scene, switch to a straight-on view, and optionally show
the calibrated camera positions and sight lines.

## What schema v3 records

The default capture is a synchronized 24-image burst: eight time targets from
`-750 ms` through `+750 ms` for each of the three cameras. Every image records
its requested offset, actual frame timestamp, actual event offset, camera
number, SHA-256 digest, semantic role, and which labeled world state it belongs
to. The camera buffers are primed before the first sample, so startup captures
have real temporal history instead of duplicated pre-roll images.

The core label is a game-independent physical world model:

- `world_before` and `world_after` contain the complete, variable-length set of
  darts believed to be on the physical board. The format deliberately permits
  zero, three, four, or more darts and does not impose visit rules.
- A dart receives a session-local stable ID while it remains in the teacher
  state. Its full-precision coordinates and segment metadata are retained.
- `transition` explicitly lists `added`, `removed`, `updated`,
  `still_present`, and `uncertain` darts. It contains no player, score, game,
  visit, or checkout concepts.
- New observations use `teacher` confidence because accepted Autodarts state
  events are the authoritative label source. Older `teacher-review-required`
  and `unlabeled` captures remain readable as excluded evidence.

The recorder captures two complementary kinds of training samples:

- Startup and periodic stable-world observations provide full board states
  across natural lighting and background changes. Periodic observations are
  recorded every five minutes by default. A capture is admitted only while
  Detection is running in `Throw`, the Board Manager state has remained
  unchanged, and no motion crosses the complete frame window.
- Accepted Board Manager state changes provide exact teacher coordinates and a
  complete before/after set transition. For every accepted dart, the recorder
  also snapshots Board Manager's raw detection state. This includes each
  camera's fitted `imageLine` from the dart tip toward its flight, vote image
  coordinates, intersections, segmentation counts, errors, and timing data.
  Seven matching Board Manager teacher images (`detection`, `before`, `after`,
  `diff`, `movement`, `skeleton`, and `export`) are retained alongside it.
  Every teacher artifact has a source, media type, and SHA-256 digest in
  `teacher_artifacts`.

Board Manager's raw dart, hand, takeout, wait, and unstable events are retained
in the bounded `context.recent_events` window on trusted samples. They never
create standalone samples and are not inferred classes. The recorder does not
invent miss, close-miss, lighting, hand, or throw labels. Exact coordinates
exist only when Autodarts emitted them.

A representative abbreviated label looks like this:

```json
{
  "schema_version": 3,
  "capture_reason": "teacher-dart-added",
  "supervision": "accepted-pseudo-positive",
  "has_coordinates": true,
  "coordinates": {"x": 0.1277737042977262, "y": 0.1502979414527876},
  "world_before": {
    "confidence": "teacher",
    "darts": []
  },
  "world_after": {
    "confidence": "teacher",
    "darts": [
      {
        "id": "session-...-dart-000001",
        "order": 1,
        "coordinates": {"x": 0.1277737042977262, "y": 0.1502979414527876},
        "segment": {"name": "S18", "number": 18, "multiplier": 1}
      }
    ]
  },
  "transition": {
    "type": "darts-added",
    "confidence": "teacher",
    "added": [{"id": "session-...-dart-000001"}]
  },
  "review_status": "unreviewed"
}
```

Each sample also retains a bounded event window, the latest event of every
observed type, and the current Board Manager state. That makes it possible to
reconstruct why a capture happened and correlate motion episodes with later
teacher observations.

At startup and after calibration, the recorder writes a setup snapshot under
`.sessions/<session-id>/<setup-id>.json`. It contains:

- Board Manager/platform versions and camera/detection statistics;
- camera resolution and irreversible camera-device fingerprints;
- per-camera board ROIs, calibration points, detected double/treble contours,
  bull position, board geometry, lens distortion, camera
  controls, and motion/detection parameters.

Authentication, API keys, board IDs, tokens, passwords, TLS keys/certificates,
hostnames/IPs, and raw camera device paths are excluded. The stable setup ID is
derived only from camera identity, geometry, and calibration inputs, not from
changing FPS statistics.

Example layout:

```text
.sessions/
└── session-20260818T120000.000000000Z/
    └── 0123456789abcdef01234567.json
20260818T120105.381000000Z-dart-2/
├── label.json
├── cam-0-frame-00.jpg
├── ...
└── cam-2-frame-07.jpg
```

`frames.before` and `frames.after` remain in `label.json` as compatibility
pointers. `frame_sequence` is the authoritative complete burst manifest.
Frame roles distinguish `stable-before`, `transition`, `stable-after`,
`stable-world`, and historical `stable-unlabeled-after` captures. Schema-v1
and schema-v2 samples remain readable. Trusted legacy teacher and board-reference
samples remain exportable, while legacy motion and any review-required or
unlabeled captures are excluded from the training inventory and WebDataset.
Their bytes still count toward quota and the dashboard reports them as excluded
evidence.

## Train on another machine

Fetch an on-demand, point-in-time WebDataset-compatible snapshot:

```console
curl --fail --output autodarts-webdataset.tar \
  http://DEVICE-IP:8090/api/export/webdataset.tar
tar -tf autodarts-webdataset.tar
```

The response streams directly from the recorded folders and does not create a
second copy on the appliance. Each key contains its JSON label, every burst
JPEG, the raw teacher detections and debug images when available, and its
sanitized setup JSON. Before download starts, the recorder checks that every
source is a regular file and verifies every recorded frame and teacher-artifact
SHA-256. The final `_autodarts_manifest.json` member has `complete: true`, exact
sample, frame, and artifact counts, and the included sample IDs. Training importers must reject
an archive without this final completion manifest or with counts that do not
match. Repeat the request later to include newly recorded play.

The service scans existing samples once at startup. New captures and quota
removals update an in-memory catalog, so opening or refreshing the dashboard
does not walk the complete image collection.

The dashboard listens on all network interfaces by default. Camera frames may
contain private surroundings and the tool has no authentication, so use it only
on a trusted LAN. Do not forward port 8090 to the internet. Use
`-listen 127.0.0.1:8090` for local-only access.

## NixOS appliance service

Enable persistent boot-time collection with:

```nix
services.autodarts-dataset.enable = true;
# Optional; default is 300 seconds.
services.autodarts-dataset.worldReferenceIntervalSeconds = 300;
```

The appliance stores samples under `/var/lib/autodarts-dataset/samples`, opens
only the configured dashboard port, starts after Autodarts Detection, and
restarts automatically after failures and reboots. It is disabled by default.

Start Detection before recording. Existing darts present when the recorder
starts become the initial physical world but are not emitted as newly accepted
darts. When the quota is exceeded, the oldest complete sample folders are
removed first.
