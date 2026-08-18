package dataset

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Dashboard struct {
	OutputDir string
	Quota     int64
}

type Inventory struct {
	UsedBytes   int64         `json:"used_bytes"`
	QuotaBytes  int64         `json:"quota_bytes"`
	SampleCount int           `json:"sample_count"`
	Samples     []SampleEntry `json:"samples"`
}

type SampleEntry struct {
	Label Label `json:"label"`
	Size  int64 `json:"size_bytes"`
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
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(inventory)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/samples/"):
		d.serveFrame(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (d Dashboard) Inventory() (Inventory, error) {
	result := Inventory{QuotaBytes: d.Quota, Samples: []SampleEntry{}}
	entries, err := os.ReadDir(d.OutputDir)
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !safeName(entry.Name()) {
			continue
		}
		directory := filepath.Join(d.OutputDir, entry.Name())
		data, err := os.ReadFile(filepath.Join(directory, "label.json"))
		if err != nil {
			continue
		}
		var label Label
		if json.Unmarshal(data, &label) != nil || label.SampleID != entry.Name() {
			continue
		}
		var size int64
		filepath.WalkDir(directory, func(_ string, item os.DirEntry, walkErr error) error {
			if walkErr == nil && !item.IsDir() {
				if info, infoErr := item.Info(); infoErr == nil {
					size += info.Size()
				}
			}
			return nil
		})
		result.UsedBytes += size
		result.Samples = append(result.Samples, SampleEntry{Label: label, Size: size})
	}
	sort.Slice(result.Samples, func(i, j int) bool {
		return result.Samples[i].Label.CapturedAt.After(result.Samples[j].Label.CapturedAt)
	})
	result.SampleCount = len(result.Samples)
	return result, nil
}

func (d Dashboard) serveFrame(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/samples/"), "/")
	if len(parts) != 2 || !safeName(parts[0]) || !safeFrameName(parts[1]) {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(d.OutputDir, parts[0], parts[1])
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
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

const dashboardHTML = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Autodarts Dataset</title><style>
*{box-sizing:border-box}body{margin:0;background:#080b10;color:#eef5f2;font:15px system-ui,sans-serif}.wrap{max-width:1400px;margin:auto;padding:32px}.brand{font-size:30px;font-weight:800}.brand span{color:#27e6a7}.summary{display:grid;grid-template-columns:repeat(3,1fr);gap:14px;margin:24px 0}.metric,.sample{background:#101720;border:1px solid #ffffff17;border-radius:16px}.metric{padding:18px}.metric strong{display:block;font-size:26px;margin-top:6px}.bar{height:8px;background:#ffffff12;border-radius:8px;overflow:hidden;margin-top:12px}.bar i{display:block;height:100%;background:#27e6a7}.samples{display:grid;gap:18px}.sample{padding:18px}.head{display:flex;justify-content:space-between;gap:12px;align-items:start}.segment{font-size:22px;font-weight:750}.coords{font-family:ui-monospace,monospace;color:#9cefd2}.frames{display:grid;grid-template-columns:repeat(3,1fr);gap:8px;margin-top:14px}.frame{position:relative}.frame img{display:block;width:100%;aspect-ratio:16/9;object-fit:cover;background:#000;border-radius:9px}.frame span{position:absolute;left:7px;bottom:6px;background:#000b;padding:3px 6px;border-radius:5px;font-size:11px}.empty{padding:50px;text-align:center;color:#aeb9c5}@media(max-width:760px){.wrap{padding:18px}.summary{grid-template-columns:1fr}.frames{grid-template-columns:1fr}.head{display:block}}
</style></head><body><main class="wrap"><div class="brand">auto<span>darts</span> dataset</div><section class="summary"><div class="metric">Samples<strong id="count">–</strong></div><div class="metric">Storage<strong id="used">–</strong><div class="bar"><i id="bar"></i></div></div><div class="metric">Quota<strong id="quota">–</strong></div></section><section id="samples" class="samples"><div class="empty">Loading samples…</div></section></main>
<script>
const size=n=>{const u=['B','KiB','MiB','GiB'];let i=0;while(n>=1024&&i<u.length-1){n/=1024;i++}return n.toFixed(i?1:0)+' '+u[i]};
const esc=s=>String(s).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
function frame(id,name,phase,i){return '<div class="frame"><img loading="lazy" src="/samples/'+encodeURIComponent(id)+'/'+encodeURIComponent(name)+'"><span>'+phase+' · cam '+i+'</span></div>'}
async function refresh(){try{const d=await fetch('/api/inventory',{cache:'no-store'}).then(r=>r.json());count.textContent=d.sample_count;used.textContent=size(d.used_bytes);quota.textContent=size(d.quota_bytes);bar.style.width=Math.min(100,d.quota_bytes?d.used_bytes/d.quota_bytes*100:0)+'%';samples.innerHTML=d.samples.length?d.samples.map(s=>{const l=s.label;return '<article class="sample"><div class="head"><div><div class="segment">'+esc(l.segment.name)+' · dart '+l.dart_index+'/'+l.dart_count+'</div><div class="coords">x='+l.coordinates.x+' · y='+l.coordinates.y+'</div></div><div>'+new Date(l.captured_at).toLocaleString()+'<br>'+size(s.size_bytes)+'</div></div><div class="frames">'+l.frames.before.map((f,i)=>frame(l.sample_id,f,'before',i)).join('')+l.frames.after.map((f,i)=>frame(l.sample_id,f,'after',i)).join('')+'</div></article>'}).join(''):'<div class="empty">No accepted darts recorded yet. Start Detection and play normally.</div>'}catch(e){samples.innerHTML='<div class="empty">Dashboard unavailable: '+esc(e.message)+'</div>'}}
refresh();setInterval(refresh,2000);
</script></body></html>`
