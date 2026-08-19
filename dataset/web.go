package dataset

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Dashboard struct {
	OutputDir string
	Quota     int64
	Catalog   *Catalog
}

type Inventory struct {
	UsedBytes     int64         `json:"used_bytes"`
	QuotaBytes    int64         `json:"quota_bytes"`
	SampleCount   int           `json:"sample_count"`
	EvidenceCount int           `json:"evidence_count"`
	EvidenceBytes int64         `json:"evidence_bytes"`
	Samples       []SampleEntry `json:"samples"`
}

type SampleEntry struct {
	Label        Label         `json:"label"`
	Size         int64         `json:"size_bytes"`
	PreviewDarts *PreviewDarts `json:"preview_darts,omitempty"`
	Preview3D    *Preview3D    `json:"preview_3d,omitempty"`
}

type webDatasetManifest struct {
	SchemaVersion int      `json:"schema_version"`
	Complete      bool     `json:"complete"`
	SampleCount   int      `json:"sample_count"`
	FrameCount    int      `json:"frame_count"`
	ArtifactCount int      `json:"artifact_count"`
	SampleIDs     []string `json:"sample_ids"`
}

func (d Dashboard) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(dashboardHTML))
	case r.Method == http.MethodGet && r.URL.Path == "/api/inventory":
		inventory, err := d.Inventory()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if limit, parseErr := strconv.Atoi(r.URL.Query().Get("limit")); parseErr == nil && limit > 0 && len(inventory.Samples) > limit {
			inventory.Samples = inventory.Samples[:limit]
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(inventory)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/setups/"):
		d.serveSetup(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/api/export/webdataset.tar":
		d.serveWebDataset(w)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/samples/"):
		d.serveFrame(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (d Dashboard) serveSetup(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/setups/"), "/")
	if len(parts) != 2 || !safeName(parts[0]) || !strings.HasSuffix(parts[1], ".json") {
		http.NotFound(w, r)
		return
	}
	setupID := strings.TrimSuffix(parts[1], ".json")
	if !safeName(setupID) {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(d.OutputDir, ".sessions", parts[0], setupID+".json")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	http.ServeFile(w, r, path)
}

func (d Dashboard) serveWebDataset(w http.ResponseWriter) {
	inventory, err := d.Inventory()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	sort.Slice(inventory.Samples, func(i, j int) bool {
		return inventory.Samples[i].Label.SampleID < inventory.Samples[j].Label.SampleID
	})
	frameCount := 0
	artifactCount := 0
	for _, sample := range inventory.Samples {
		frames, artifacts, err := d.validateWebDatasetSample(sample.Label)
		if err != nil {
			http.Error(w, fmt.Sprintf("dataset export validation failed for %s: %v", sample.Label.SampleID, err), http.StatusInternalServerError)
			return
		}
		frameCount += frames
		artifactCount += artifacts
	}
	w.Header().Set("Content-Type", "application/x-tar")
	w.Header().Set("Content-Disposition", `attachment; filename="autodarts-webdataset.tar"`)
	w.Header().Set("X-Autodarts-Sample-Count", fmt.Sprint(inventory.SampleCount))
	w.Header().Set("X-Autodarts-Completion-Manifest", "_autodarts_manifest.json")
	archive := tar.NewWriter(w)
	defer archive.Close()
	sampleIDs := make([]string, 0, len(inventory.Samples))
	for _, sample := range inventory.Samples {
		if err := d.writeWebDatasetSample(archive, sample.Label); err != nil {
			return
		}
		sampleIDs = append(sampleIDs, sample.Label.SampleID)
	}
	manifest, err := json.Marshal(webDatasetManifest{SchemaVersion: 1, Complete: true, SampleCount: len(sampleIDs), FrameCount: frameCount, ArtifactCount: artifactCount, SampleIDs: sampleIDs})
	if err != nil {
		return
	}
	_ = writeTarFile(archive, "_autodarts_manifest.json", manifest, time.Now().UTC())
}

func (d Dashboard) validateWebDatasetSample(label Label) (int, int, error) {
	if !safeName(label.SampleID) {
		return 0, 0, errors.New("unsafe sample identity")
	}
	directory := filepath.Join(d.OutputDir, label.SampleID)
	if err := requireRegularFile(filepath.Join(directory, "label.json")); err != nil {
		return 0, 0, err
	}
	expectedHashes := make(map[string]string, len(label.FrameSequence))
	for _, frame := range label.FrameSequence {
		expectedHashes[frame.File] = frame.SHA256
	}
	frameNames := make([]string, 0, len(label.FrameSequence)+len(label.Frames.Before)+len(label.Frames.After))
	for _, frame := range label.FrameSequence {
		frameNames = append(frameNames, frame.File)
	}
	if len(frameNames) == 0 {
		frameNames = append(frameNames, label.Frames.Before...)
		frameNames = append(frameNames, label.Frames.After...)
	}
	seen := make(map[string]bool)
	for _, name := range frameNames {
		if seen[name] {
			continue
		}
		seen[name] = true
		if !safeFrameName(name) {
			return 0, 0, fmt.Errorf("unsafe frame name %q", name)
		}
		path := filepath.Join(directory, name)
		if err := requireRegularFile(path); err != nil {
			return 0, 0, err
		}
		if expected := expectedHashes[name]; expected != "" {
			data, err := os.ReadFile(path)
			if err != nil {
				return 0, 0, err
			}
			actual := fmt.Sprintf("%x", sha256.Sum256(data))
			if actual != expected {
				return 0, 0, fmt.Errorf("frame %q checksum mismatch", name)
			}
		}
	}
	for _, artifact := range label.TeacherArtifacts {
		if !safeArtifactName(artifact.File) {
			return 0, 0, fmt.Errorf("unsafe teacher artifact name %q", artifact.File)
		}
		path := filepath.Join(directory, artifact.File)
		if err := requireRegularFile(path); err != nil {
			return 0, 0, err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return 0, 0, err
		}
		if fmt.Sprintf("%x", sha256.Sum256(data)) != artifact.SHA256 {
			return 0, 0, fmt.Errorf("teacher artifact %q checksum mismatch", artifact.File)
		}
	}
	if label.SessionID != "" || label.SetupID != "" {
		if !safeName(label.SessionID) || !safeName(label.SetupID) {
			return 0, 0, errors.New("unsafe setup identity")
		}
		if err := requireRegularFile(filepath.Join(d.OutputDir, ".sessions", label.SessionID, label.SetupID+".json")); err != nil {
			return 0, 0, err
		}
	}
	return len(seen), len(label.TeacherArtifacts), nil
}

func requireRegularFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", filepath.Base(path))
	}
	return nil
}

func (d Dashboard) writeWebDatasetSample(archive *tar.Writer, label Label) error {
	directory := filepath.Join(d.OutputDir, label.SampleID)
	type archivedFrame struct {
		name string
		data []byte
	}
	var frames []archivedFrame
	exported := label
	exported.FrameSequence = append([]CapturedFrame(nil), label.FrameSequence...)
	exported.TeacherArtifacts = append([]TeacherArtifact(nil), label.TeacherArtifacts...)
	renames := make(map[string]string)
	frameNames := make([]string, 0, len(label.FrameSequence)+len(label.Frames.Before)+len(label.Frames.After))
	for _, frame := range label.FrameSequence {
		frameNames = append(frameNames, frame.File)
	}
	if len(frameNames) == 0 {
		frameNames = append(frameNames, label.Frames.Before...)
		frameNames = append(frameNames, label.Frames.After...)
	}
	seen := make(map[string]bool)
	expectedHashes := make(map[string]string, len(label.FrameSequence))
	for _, frame := range label.FrameSequence {
		expectedHashes[frame.File] = frame.SHA256
	}
	for _, name := range frameNames {
		if seen[name] {
			continue
		}
		seen[name] = true
		if !safeFrameName(name) {
			return fmt.Errorf("unsafe frame name %q", name)
		}
		data, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			return err
		}
		if expected := expectedHashes[name]; expected != "" && fmt.Sprintf("%x", sha256.Sum256(data)) != expected {
			return fmt.Errorf("frame %q checksum changed during export", name)
		}
		exportName := label.SampleID + "." + name
		renames[name] = exportName
		frames = append(frames, archivedFrame{name: exportName, data: data})
	}
	for index := range exported.FrameSequence {
		exported.FrameSequence[index].File = renames[exported.FrameSequence[index].File]
	}
	var artifacts []archivedFrame
	for index, artifact := range label.TeacherArtifacts {
		if !safeArtifactName(artifact.File) {
			return fmt.Errorf("unsafe teacher artifact name %q", artifact.File)
		}
		data, err := os.ReadFile(filepath.Join(directory, artifact.File))
		if err != nil {
			return err
		}
		if fmt.Sprintf("%x", sha256.Sum256(data)) != artifact.SHA256 {
			return fmt.Errorf("teacher artifact %q checksum changed during export", artifact.File)
		}
		exportName := label.SampleID + "." + artifact.File
		exported.TeacherArtifacts[index].File = exportName
		artifacts = append(artifacts, archivedFrame{name: exportName, data: data})
	}
	exported.Frames = FrameFiles{
		Before: make([]string, 0, len(label.Frames.Before)),
		After:  make([]string, 0, len(label.Frames.After)),
	}
	for _, group := range []struct {
		source []string
		target *[]string
	}{{label.Frames.Before, &exported.Frames.Before}, {label.Frames.After, &exported.Frames.After}} {
		for _, name := range group.source {
			if !safeFrameName(name) {
				return fmt.Errorf("unsafe frame name %q", name)
			}
			exportName, ok := renames[name]
			if !ok {
				return fmt.Errorf("legacy frame %q absent from frame sequence", name)
			}
			*group.target = append(*group.target, exportName)
		}
	}
	var setup archivedFrame
	if label.SessionID != "" && label.SetupID != "" {
		if !safeName(label.SessionID) || !safeName(label.SetupID) {
			return errors.New("unsafe setup identity")
		}
		data, err := os.ReadFile(filepath.Join(d.OutputDir, ".sessions", label.SessionID, label.SetupID+".json"))
		if err != nil {
			return err
		}
		setup = archivedFrame{name: label.SampleID + ".setup.json", data: data}
		exported.SetupFile = setup.name
	}
	metadata, err := json.Marshal(exported)
	if err != nil {
		return err
	}
	if err := writeTarFile(archive, label.SampleID+".json", metadata, label.CapturedAt); err != nil {
		return err
	}
	for _, frame := range frames {
		if err := writeTarFile(archive, frame.name, frame.data, label.CapturedAt); err != nil {
			return err
		}
	}
	for _, artifact := range artifacts {
		if err := writeTarFile(archive, artifact.name, artifact.data, label.CapturedAt); err != nil {
			return err
		}
	}
	if setup.name != "" {
		if err := writeTarFile(archive, setup.name, setup.data, label.CapturedAt); err != nil {
			return err
		}
	}
	return nil
}

func writeTarFile(archive *tar.Writer, name string, data []byte, modified time.Time) error {
	if err := archive.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(data)), ModTime: modified}); err != nil {
		return err
	}
	_, err := archive.Write(data)
	return err
}

func (d Dashboard) Inventory() (Inventory, error) {
	if d.Catalog != nil {
		return d.Catalog.Inventory(), nil
	}
	stored, used, err := scanStoredSamples(d.OutputDir)
	if err != nil {
		return Inventory{}, err
	}
	result := Inventory{UsedBytes: used, QuotaBytes: d.Quota, Samples: []SampleEntry{}}
	for _, sample := range stored {
		if !sample.trainingReady {
			result.EvidenceCount++
			result.EvidenceBytes += sample.entry.Size
			continue
		}
		result.Samples = append(result.Samples, sample.entry)
	}
	sort.Slice(result.Samples, func(i, j int) bool {
		return result.Samples[i].Label.CapturedAt.After(result.Samples[j].Label.CapturedAt)
	})
	result.SampleCount = len(result.Samples)
	return result, nil
}

func isTrainingReady(label Label) bool {
	if label.ReviewStatus == "required" {
		return false
	}
	switch label.Supervision {
	case "accepted-pseudo-positive", "board-reference", "teacher-world-state":
	default:
		return false
	}
	if label.WorldAfter != nil && label.WorldAfter.Confidence != "" && label.WorldAfter.Confidence != WorldConfidenceTeacher {
		return false
	}
	if label.Transition != nil && label.Transition.Confidence != "" && label.Transition.Confidence != WorldConfidenceTeacher {
		return false
	}
	return true
}

func normalizeLegacyLabel(label *Label) {
	if label.SchemaVersion < 2 && label.LabelSource == "autodarts" && label.Segment.Name != "" {
		label.HasCoordinates = true
		if label.Supervision == "" {
			label.Supervision = "accepted-pseudo-positive"
		}
	}
	if label.SchemaVersion < 3 && label.CaptureReason == "" {
		switch label.Supervision {
		case "accepted-pseudo-positive":
			label.CaptureReason = "legacy-teacher-dart"
		case "board-reference":
			label.CaptureReason = "legacy-board-reference"
		case "unlabeled-motion":
			label.CaptureReason = "legacy-motion"
		default:
			label.CaptureReason = "legacy-sample"
		}
	}
}

func (d Dashboard) serveFrame(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/samples/"), "/")
	if len(parts) != 2 || !safeName(parts[0]) || !(safeFrameName(parts[1]) || safeArtifactName(parts[1])) {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(d.OutputDir, parts[0], parts[1])
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	if strings.HasSuffix(parts[1], ".json") {
		w.Header().Set("Content-Type", "application/json")
	} else {
		w.Header().Set("Content-Type", "image/jpeg")
	}
	http.ServeFile(w, r, path)
}

func safeName(value string) bool {
	if value == "" || value == "." || value == ".." {
		return false
	}
	for _, char := range value {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '-' || char == '.' || char == '_') {
			return false
		}
	}
	return true
}

func safeFrameName(value string) bool {
	return safeName(value) && strings.HasPrefix(value, "cam-") && strings.HasSuffix(value, ".jpg")
}

func safeArtifactName(value string) bool {
	return safeName(value) && strings.HasPrefix(value, "teacher-") && (strings.HasSuffix(value, ".jpg") || strings.HasSuffix(value, ".json"))
}

const dashboardHTML = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Autodarts Dataset</title><style>
*{box-sizing:border-box}body{margin:0;background:#080b10;color:#eef5f2;font:15px system-ui,sans-serif}.wrap{max-width:1400px;margin:auto;padding:32px}.brand{font-size:30px;font-weight:800}.brand span{color:#27e6a7}.summary{display:grid;grid-template-columns:repeat(3,1fr);gap:14px;margin:24px 0}.metric,.sample{background:#101720;border:1px solid #ffffff17;border-radius:16px}.metric{padding:18px}.metric strong{display:block;font-size:26px;margin-top:6px}.bar{height:8px;background:#ffffff12;border-radius:8px;overflow:hidden;margin-top:12px}.bar i{display:block;height:100%;background:#27e6a7}.samples{display:grid;gap:18px}.sample{padding:18px}.head{display:flex;justify-content:space-between;gap:12px;align-items:start}.segment{font-size:22px;font-weight:750}.coords,.world{font-family:ui-monospace,monospace;color:#9cefd2;margin-top:4px}.review{color:#ffc96b;font-weight:700}.frames{display:grid;grid-template-columns:repeat(3,1fr);gap:8px;margin-top:14px}.frame{position:relative}.frame img{display:block;width:100%;aspect-ratio:16/9;object-fit:cover;background:#000;border-radius:9px}.frame span{position:absolute;left:7px;bottom:6px;background:#000b;padding:3px 6px;border-radius:5px;font-size:11px}.empty{padding:50px;text-align:center;color:#aeb9c5}a{color:#27e6a7}@media(max-width:760px){.wrap{padding:18px}.summary{grid-template-columns:1fr}.frames{grid-template-columns:1fr}.head{display:block}}
.summary{grid-template-columns:repeat(4,1fr)}
.toolbar{display:flex;align-items:center;justify-content:space-between;gap:16px;margin:0 0 18px;color:#aeb9c5}.overlay-controls{display:flex;align-items:center;gap:18px;flex-wrap:wrap}.toggle{display:flex;align-items:center;gap:8px;color:#eef5f2;cursor:pointer}.toggle input{accent-color:#27e6a7}.legend{display:flex;gap:10px;flex-wrap:wrap;font-size:12px}.legend i{display:inline-block;width:11px;height:11px;border-radius:50%;margin-right:4px;vertical-align:-1px}.legend .roi-key{background:#00d9ff}.legend .double-key{background:#00ff8c}.legend .treble-key{background:#ff3df2}.legend .bull-key{background:#ffe600}.legend .calibration-key{background:#b982ff}.legend .dart-key{background:#ff334f}.legend .line-key{background:#ff9f1c}.frame svg.features{position:absolute;inset:0;width:100%;height:100%;pointer-events:none;filter:drop-shadow(0 0 3px #000) drop-shadow(0 0 2px #000)}.features .roi{fill:none;stroke:#00d9ff;stroke-width:6;stroke-dasharray:18 10}.features .sector{stroke:#39bfff;stroke-width:3;stroke-dasharray:7 5;opacity:.8}.features .double{fill:none;stroke:#00ff8c;stroke-width:6}.features .treble{fill:none;stroke:#ff3df2;stroke-width:6}.features .double-node{fill:#00ff8c;stroke:#071017;stroke-width:3}.features .treble-node{fill:#ff3df2;stroke:#071017;stroke-width:3}.features .bull{fill:#ffe60088;stroke:#ffe600;stroke-width:7}.features .calibration{fill:#b982ff;stroke:#fff;stroke-width:4}.features .dart-halo{fill:#ff334fcc;stroke:#fff;stroke-width:5}.features .dart-cross{stroke:#fff;stroke-width:4}.features .dart-number{fill:#fff;font:bold 18px system-ui,sans-serif;text-anchor:middle;dominant-baseline:central}.features .teacher-line{stroke:#ff9f1c;stroke-width:8;stroke-linecap:round}.features .teacher-tip{fill:#ff9f1c;stroke:#fff;stroke-width:5}.hide-features .features{display:none}
.world3d{margin-top:14px;border:1px solid #ffffff1d;border-radius:12px;overflow:hidden;background:radial-gradient(circle at 50% 30%,#172430,#080c12 70%)}.world3d-head{display:flex;justify-content:space-between;gap:12px;padding:11px 14px;color:#aeb9c5;font-size:13px}.world3d-head strong{color:#eef5f2}.world3d canvas{display:block;width:100%;height:420px;touch-action:none;cursor:grab}.world3d canvas:active{cursor:grabbing}.world3d-note{padding:0 14px 12px;color:#8e9dab;font-size:12px}
</style></head><body><main class="wrap"><div class="brand">auto<span>darts</span> dataset</div><p><a href="/api/export/webdataset.tar">Download training-ready WebDataset</a></p><section class="summary"><div class="metric">Training samples<strong id="count">–</strong></div><div class="metric">Excluded evidence<strong id="evidence">–</strong></div><div class="metric">Storage<strong id="used">–</strong><div class="bar"><i id="bar"></i></div></div><div class="metric">Quota<strong id="quota">–</strong></div></section><div class="toolbar"><span>Accepted throws first, then the latest world observations</span><div class="overlay-controls"><div class="legend"><span><i class="roi-key"></i>ROI</span><span><i class="double-key"></i>Double</span><span><i class="treble-key"></i>Treble</span><span><i class="bull-key"></i>Bull</span><span><i class="calibration-key"></i>Calibration</span><span><i class="dart-key"></i>Teacher dart</span><span><i class="line-key"></i>Tip-flight line</span></div><label class="toggle"><input id="featuresToggle" type="checkbox" checked>Detected board features</label></div></div><section id="samples" class="samples"><div class="empty">Loading samples…</div></section></main>
<script>
const size=n=>{const u=['B','KiB','MiB','GiB'];let i=0;while(n>=1024&&i<u.length-1){n/=1024;i++}return n.toFixed(i?1:0)+' '+u[i]};
const esc=s=>String(s).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
function isMotion(l){return l.capture_reason==='motion-started'||l.capture_reason==='motion-settled'}
function framePhase(l,phase){if(l.capture_reason==='motion-started')return phase==='before'?'pre-motion (known before)':'motion in progress (unlabeled)';if(l.capture_reason==='motion-settled')return phase==='before'?'pre-settle (unlabeled)':'post-settle (unlabeled)';if(l.capture_reason==='startup-world-observation'||l.capture_reason==='periodic-world-observation')return 'stable world';if(l.schema_version>=3)return phase==='before'?'world before':'world after';return phase}
const setupCache=new Map();
async function setupFor(l){if(!l.session_id||!l.setup_id)return null;const key=l.session_id+'/'+l.setup_id;if(!setupCache.has(key))setupCache.set(key,fetch('/api/setups/'+encodeURIComponent(l.session_id)+'/'+encodeURIComponent(l.setup_id)+'.json',{cache:'no-store'}).then(r=>r.ok?r.json():null).catch(()=>null));return setupCache.get(key)}
const teacherCache=new Map();
async function teacherLinesFor(l){const artifact=(l.teacher_artifacts||[]).find(a=>a.kind==='detections');if(!artifact)return [];const key=l.sample_id+'/'+artifact.file;if(!teacherCache.has(key))teacherCache.set(key,fetch('/samples/'+encodeURIComponent(l.sample_id)+'/'+encodeURIComponent(artifact.file),{cache:'no-store'}).then(r=>r.ok?r.json():[]).then(results=>(Array.isArray(results)?results:[]).flatMap((result,dart)=>(Array.isArray(result?.detections)?result.detections:[]).map((detection,camera)=>({camera,dart:dart+1,...detection?.imageLine}))).filter(line=>['x1','y1','x2','y2'].every(key=>Number.isFinite(Number(line[key]))))).catch(()=>[]));return teacherCache.get(key)}
const finite=v=>Number.isFinite(Number(v))?Number(v):0;
const pointList=a=>(Array.isArray(a)?a:[]).map(p=>finite(p?.[0])+','+finite(p?.[1])).join(' ');
function boardOverlay(setup,camera,darts,teacherLines){const config=setup?.config||{};const board=config?.dartboard?.[camera];const roi=config?.board_rois?.[camera];const calibration=config?.calibration?.[camera];if(!board&&!roi&&!calibration&&!darts?.length&&!teacherLines?.length)return '';const width=finite(setup?.camera_resolution?.width||config?.cam?.width||1280);const height=finite(setup?.camera_resolution?.height||config?.cam?.height||720);let shapes='';if(roi)shapes+='<rect class="roi" x="'+finite(roi.x)+'" y="'+finite(roi.y)+'" width="'+finite(roi.width)+'" height="'+finite(roi.height)+'"/>';if(board?.bull)for(const p of Array.isArray(board?.doubleOuters)?board.doubleOuters:[])shapes+='<line class="sector" x1="'+finite(board.bull[0])+'" y1="'+finite(board.bull[1])+'" x2="'+finite(p?.[0])+'" y2="'+finite(p?.[1])+'"/>';for(const name of ['doubleOuters','doubleInner'])if(board?.[name])shapes+='<polygon class="double" points="'+pointList(board[name])+'"/>';for(const name of ['trebleOuters','trebleInner'])if(board?.[name])shapes+='<polygon class="treble" points="'+pointList(board[name])+'"/>';for(const p of Array.isArray(board?.doubleOuters)?board.doubleOuters:[])shapes+='<circle class="double-node" cx="'+finite(p?.[0])+'" cy="'+finite(p?.[1])+'" r="6"/>';for(const p of Array.isArray(board?.trebleOuters)?board.trebleOuters:[])shapes+='<circle class="treble-node" cx="'+finite(p?.[0])+'" cy="'+finite(p?.[1])+'" r="6"/>';if(board?.bull)shapes+='<circle class="bull" cx="'+finite(board.bull[0])+'" cy="'+finite(board.bull[1])+'" r="13"/>';for(const p of Array.isArray(calibration)?calibration:[])shapes+='<circle class="calibration" cx="'+finite(p?.[0])*width+'" cy="'+finite(p?.[1])*height+'" r="10"/>';for(const line of Array.isArray(teacherLines)?teacherLines.filter(line=>line.camera===camera):[])shapes+='<g class="teacher-detection-line"><title>Board Manager dart '+finite(line.dart)+' tip-to-flight line</title><line class="teacher-line" x1="'+finite(line.x1)+'" y1="'+finite(line.y1)+'" x2="'+finite(line.x2)+'" y2="'+finite(line.y2)+'"/><circle class="teacher-tip" cx="'+finite(line.x2)+'" cy="'+finite(line.y2)+'" r="10"/></g>';for(const dart of Array.isArray(darts)?darts.filter(d=>d.camera===camera):[])shapes+='<g class="dart-marker"><title>Teacher dart '+finite(dart.order)+' · '+esc(dart.segment)+'</title><circle class="dart-halo" cx="'+finite(dart.x)+'" cy="'+finite(dart.y)+'" r="18"/><path class="dart-cross" d="M '+(finite(dart.x)-27)+' '+finite(dart.y)+' H '+(finite(dart.x)+27)+' M '+finite(dart.x)+' '+(finite(dart.y)-27)+' V '+(finite(dart.y)+27)+'"/><text class="dart-number" x="'+finite(dart.x)+'" y="'+finite(dart.y)+'">'+finite(dart.order)+'</text></g>';return '<svg class="features" role="img" aria-label="Detected dartboard features, teacher darts, and tip-to-flight lines" viewBox="0 0 '+width+' '+height+'" preserveAspectRatio="none">'+shapes+'</svg>'}
function frame(l,name,phase,i,setup,darts,teacherLines){return name?'<div class="frame"><img loading="lazy" src="/samples/'+encodeURIComponent(l.sample_id)+'/'+encodeURIComponent(name)+'">'+boardOverlay(setup,i,darts,teacherLines)+'<span>'+framePhase(l,phase)+' · cam '+i+'</span></div>':''}
const vadd=(a,b)=>a.map((v,i)=>v+b[i]),vsub=(a,b)=>a.map((v,i)=>v-b[i]),vscale=(a,s)=>a.map(v=>v*s),vdot=(a,b)=>a.reduce((s,v,i)=>s+v*b[i],0),vcross=(a,b)=>[a[1]*b[2]-a[2]*b[1],a[2]*b[0]-a[0]*b[2],a[0]*b[1]-a[1]*b[0]],vnorm=a=>{const n=Math.hypot(...a);return n?vscale(a,1/n):[0,0,0]};
function world3DCard(l,scene){if(!scene?.darts?.length)return '';const worstFit=Math.max(...scene.darts.map(d=>finite(d.fit_residual_degrees)));return '<section class="world3d"><div class="world3d-head"><strong>Reconstructed 3D board world</strong><span>'+scene.darts.length+' physical dart'+(scene.darts.length===1?'':'s')+' · 3 calibrated cameras · worst fit '+worstFit.toFixed(2)+'°</span></div><canvas data-world3d="'+esc(l.sample_id)+'" role="img" aria-label="Interactive 3D reconstruction of '+scene.darts.length+' physical darts"><p>3D preview requires canvas.</p></canvas><div class="world3d-note">Drag to orbit · wheel or pinch to zoom · directions are reconstructed; displayed dart length is '+finite(scene.darts[0].length_mm)+' mm.</div></section>'}
function mountWorld3D(canvas,scene){const ctx=canvas.getContext('2d');if(!ctx)return;let yaw=-.65,pitch=.32,distance=720,drag=null;const projectFactory=(width,height)=>{const target=[0,0,35],position=[Math.sin(yaw)*Math.cos(pitch)*distance,Math.sin(pitch)*distance,Math.cos(yaw)*Math.cos(pitch)*distance],forward=vnorm(vsub(target,position)),right=vnorm(vcross(forward,[0,1,0])),up=vcross(right,forward),f=Math.min(width,height)*1.05;return p=>{const relative=vsub(p,position),depth=vdot(relative,forward);return [width/2+vdot(relative,right)*f/depth,height/2-vdot(relative,up)*f/depth,depth]}};const path=(project,points,close=false)=>{ctx.beginPath();points.forEach((p,i)=>{const q=project(p);i?ctx.lineTo(q[0],q[1]):ctx.moveTo(q[0],q[1])});if(close)ctx.closePath()};const circle=(project,r,z=0,steps=80)=>Array.from({length:steps},(_,i)=>[Math.sin(i/steps*Math.PI*2)*r,Math.cos(i/steps*Math.PI*2)*r,z]);const draw=()=>{const rect=canvas.getBoundingClientRect(),ratio=Math.min(2,devicePixelRatio||1),width=Math.max(1,Math.round(rect.width*ratio)),height=Math.max(1,Math.round(rect.height*ratio));if(canvas.width!==width||canvas.height!==height){canvas.width=width;canvas.height=height}ctx.clearRect(0,0,width,height);const project=projectFactory(width,height);ctx.lineJoin='round';ctx.fillStyle='#090d12';path(project,circle(project,scene.board_radius_mm,-18),true);ctx.fill();ctx.strokeStyle='#49535b';ctx.lineWidth=16*ratio;ctx.stroke();ctx.fillStyle='#111820';path(project,circle(project,scene.board_radius_mm,0),true);ctx.fill();for(const [radius,color,lineWidth] of [[170,'#e9edf0',4],[162,'#2ce89f',7],[107,'#e94689',7],[99,'#e94689',5],[15.9,'#2ce89f',8],[6.35,'#ff405d',10]]){ctx.strokeStyle=color;ctx.lineWidth=lineWidth*ratio;path(project,circle(project,radius));ctx.stroke()}ctx.strokeStyle='#77838e';ctx.lineWidth=1.4*ratio;for(let i=0;i<20;i++){const a=(i+.5)/20*Math.PI*2;path(project,[[0,0,.5],[Math.sin(a)*170,Math.cos(a)*170,.5]]);ctx.stroke()}for(const camera of scene.cameras||[]){const center=camera.center_mm;ctx.strokeStyle=['#00d9ff55','#b982ff55','#ffe60055'][camera.camera%3];ctx.lineWidth=1*ratio;ctx.setLineDash([7*ratio,7*ratio]);for(const dart of scene.darts){path(project,[center,dart.tip_mm]);ctx.stroke()}ctx.setLineDash([]);const q=project(center);ctx.fillStyle=['#00d9ff','#b982ff','#ffe600'][camera.camera%3];ctx.beginPath();ctx.arc(q[0],q[1],6*ratio,0,Math.PI*2);ctx.fill();ctx.fillStyle='#dfe8ee';ctx.font=12*ratio+'px system-ui';ctx.fillText('cam '+camera.camera,q[0]+9*ratio,q[1]-7*ratio)}for(const dart of scene.darts){const tip=dart.tip_mm,flight=dart.flight_mm,tailDir=vnorm(vsub(flight,tip)),side=vnorm(vcross(tailDir,[0,1,0])),finBase=vsub(flight,vscale(tailDir,28));ctx.strokeStyle='#ff9f1c';ctx.lineWidth=7*ratio;path(project,[tip,flight]);ctx.stroke();ctx.fillStyle='#ff405d';let q=project(tip);ctx.beginPath();ctx.arc(q[0],q[1],8*ratio,0,Math.PI*2);ctx.fill();ctx.fillStyle='#ff9f1c99';for(const sign of [-1,1]){path(project,[flight,vadd(finBase,vscale(side,20*sign)),vsub(flight,vscale(tailDir,8))],true);ctx.fill()}q=project(flight);ctx.fillStyle='#fff';ctx.font='bold '+14*ratio+'px system-ui';ctx.fillText(dart.segment,q[0]+10*ratio,q[1]-10*ratio)}};const point=e=>[e.clientX,e.clientY];canvas.addEventListener('pointerdown',e=>{drag=point(e);canvas.setPointerCapture(e.pointerId)});canvas.addEventListener('pointermove',e=>{if(!drag)return;const next=point(e);yaw+=(next[0]-drag[0])*.008;pitch=Math.max(-1.25,Math.min(1.25,pitch+(next[1]-drag[1])*.008));drag=next;draw()});canvas.addEventListener('pointerup',()=>drag=null);canvas.addEventListener('pointercancel',()=>drag=null);canvas.addEventListener('wheel',e=>{e.preventDefault();distance=Math.max(330,Math.min(1500,distance*Math.exp(e.deltaY*.001)));draw()},{passive:false});new ResizeObserver(draw).observe(canvas);draw()}
function mountWorldPreviews(entries){const scenes=new Map(entries.filter(s=>s.preview_3d).map(s=>[s.label.sample_id,s.preview_3d]));document.querySelectorAll('canvas[data-world3d]').forEach(canvas=>{const scene=scenes.get(canvas.dataset.world3d);if(scene)mountWorld3D(canvas,scene)})}
function title(l){if(l.capture_reason==='motion-started')return 'Motion started · resulting board unknown';if(l.capture_reason==='motion-settled')return 'Motion settled · resulting board unknown';if(l.has_coordinates)return esc(l.segment.name)+' · dart '+l.dart_index+'/'+l.dart_count;if(l.capture_reason==='startup-world-observation')return 'Startup observation · '+l.world_after.darts.length+' darts';if(l.capture_reason==='periodic-world-observation')return 'Periodic observation · '+l.world_after.darts.length+' darts';if(l.world_after)return 'Physical board · '+l.world_after.darts.length+' darts';if(l.world_before)return 'Known board before event · '+l.world_before.darts.length+' darts';if(l.capture_reason==='legacy-board-reference')return 'Board reference (schema v2)';if(l.capture_reason==='legacy-motion')return 'Unlabeled motion (schema v2)';return esc(l.supervision||l.label_source)}
function reasonLabel(l){if(isMotion(l))return 'Unlabeled physical transition';if(l.capture_reason==='startup-world-observation')return 'Complete physical board when the recorder started';if(l.capture_reason==='periodic-world-observation')return 'Periodic stable physical-board snapshot';if(l.capture_reason==='teacher-world-reconciled')return 'Autodarts world reconciliation';return esc(l.capture_reason||l.supervision||l.label_source)}
function worldLine(prefix,w){return '<div class="world">'+prefix+': '+esc(w.confidence)+' · '+w.darts.length+' physical darts</div>'}
function details(l){const t=l.transition;let v=l.has_coordinates?'<div class="coords">x='+l.coordinates.x+' · y='+l.coordinates.y+'</div>':'<div class="coords">'+reasonLabel(l)+'</div>';if(isMotion(l)){if(l.world_before)v+=worldLine('Before motion',l.world_before);v+='<div class="review">After motion: unknown — inspect the unlabeled frames</div>';if(t?.evidence?.length)v+='<div class="world">Raw signals: '+esc(t.evidence.join(', '))+'</div>'}else{if(l.world_before)v+=worldLine('Before',l.world_before);if(l.world_after)v+=worldLine(l.world_before?'After':'Observed world',l.world_after);if(t&&t.type!=='world-observed')v+='<div class="world">'+esc(t.type)+' · +'+(t.added?.length||0)+' −'+(t.removed?.length||0)+' ='+(t.still_present?.length||0)+' ?'+(t.uncertain?.length||0)+'</div>'}if(l.review_status==='required')v+='<div class="review">Review required</div>';return v}
async function sampleCard(s){const l=s.label,[setup,teacherLines]=await Promise.all([setupFor(l),teacherLinesFor(l)]);const before=(l.frames?.before||[]).map((f,i)=>frame(l,f,'before',i,setup,s.preview_darts?.before,[])).join('');const after=(l.frames?.after||[]).map((f,i)=>frame(l,f,'after',i,setup,s.preview_darts?.after,teacherLines)).join('');const orderedFrames=l.has_coordinates?after+before:before+after;return '<article class="sample"><div class="head"><div><div class="segment">'+title(l)+'</div>'+details(l)+'</div><div>'+new Date(l.captured_at).toLocaleString()+'<br>'+size(s.size_bytes)+' · '+(l.frame_sequence?.length||6)+' frames</div></div>'+world3DCard(l,s.preview_3d)+'<div class="frames">'+orderedFrames+'</div></article>'}
async function refresh(){try{const d=await fetch('/api/inventory?limit=24',{cache:'no-store'}).then(r=>r.json());count.textContent=d.sample_count;evidence.textContent=d.evidence_count;used.textContent=size(d.used_bytes);quota.textContent=size(d.quota_bytes);bar.style.width=Math.min(100,d.quota_bytes?d.used_bytes/d.quota_bytes*100:0)+'%';const preview=[...d.samples].sort((a,b)=>Number(b.label.has_coordinates)-Number(a.label.has_coordinates)||new Date(b.label.captured_at)-new Date(a.label.captured_at));samples.innerHTML=preview.length?(await Promise.all(preview.map(sampleCard))).join(''):'<div class="empty">No training-ready samples recorded yet. Start Detection and play normally.</div>';mountWorldPreviews(preview)}catch(e){samples.innerHTML='<div class="empty">Dashboard unavailable: '+esc(e.message)+'</div>'}}
featuresToggle.addEventListener('change',()=>document.body.classList.toggle('hide-features',!featuresToggle.checked));refresh();setInterval(refresh,2000);
</script></body></html>`
