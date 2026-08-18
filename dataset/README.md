# Autodarts accepted-dart dataset recorder

`autodarts-dataset` records accepted darts from regular gameplay. It listens to
Board Manager events and buffers Board Manager's three MJPEG camera streams. It
does not open `/dev/video*`, alter detection settings, or upload any data.

Run it on the appliance or another machine that can reach Board Manager:

```console
go run ./cmd/autodarts-dataset \
  -url http://127.0.0.1:3180 \
  -output ./autodarts-dataset \
  -quota-gb 20 \
  -listen 0.0.0.0:8090
```

Open `http://DEVICE-IP:8090` while the recorder is running. The dashboard
updates every two seconds and shows sample count, quota usage, exact
coordinates, segment metadata, and all three before/after camera pairs.

The dashboard listens on all network interfaces by default. Camera frames may
contain private surroundings and the tool has no authentication, so use it only
on a trusted LAN. Do not forward port 8090 to the internet. Use
`-listen 127.0.0.1:8090` when local-only access is preferred. The included NixOS
appliance configuration allows inbound TCP port 8090; other hosts may require a
firewall rule.

On the NixOS appliance, enable persistent boot-time collection with:

```nix
services.autodarts-dataset.enable = true;
```

The service stores samples under `/var/lib/autodarts-dataset/samples`, opens
only its configured dashboard port, starts after Autodarts Detection, and
restarts automatically after failures and reboots. It is disabled by default.

Start Detection before recording. Existing darts already present when the tool
starts are ignored. Each newly accepted dart creates one atomic sample folder:

```text
20260813T184205.381000000Z-dart-2/
├── label.json
├── cam-0-before.jpg
├── cam-1-before.jpg
├── cam-2-before.jpg
├── cam-0-after.jpg
├── cam-1-after.jpg
└── cam-2-after.jpg
```

Example label:

```json
{
  "schema_version": 1,
  "sample_id": "20260813T184205.381000000Z-dart-2",
  "captured_at": "2026-08-13T18:42:05.381Z",
  "label_source": "autodarts",
  "coordinates": {
    "x": 0.1277737042977262,
    "y": 0.1502979414527876
  },
  "segment": {
    "name": "S18",
    "number": 18,
    "bed": "SingleInner",
    "multiplier": 1
  },
  "dart_index": 2,
  "dart_count": 2,
  "frames": {
    "before": ["cam-0-before.jpg", "cam-1-before.jpg", "cam-2-before.jpg"],
    "after": ["cam-0-after.jpg", "cam-1-after.jpg", "cam-2-after.jpg"]
  },
  "review_status": "unreviewed"
}
```

`coordinates` is the authoritative full-precision label reported by Autodarts.
`segment` is retained as validation metadata. The tool intentionally does not
create missed-dart, close-dart, hand, lighting, or other inferred labels.

The default frame targets are 250 ms before and 350 ms after the accepted-dart
event. Use `-before` and `-after` to tune them. When the quota is exceeded, the
oldest complete sample folders are removed first.
