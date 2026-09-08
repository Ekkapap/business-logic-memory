package blm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// WriteGraphHTML เขียน <store>/graph.html — หน้าเดียว เปิดในเบราว์เซอร์ดูความสัมพันธ์ (เจ้าของถาม 2026-09-09)
// ใช้ d3-force จาก CDN (ไฟล์อยู่ในเครื่อง เปิดออนไลน์ได้) โหนด = ไฟล์ สีตาม cluster ขนาดตาม in-degree เส้นทึบ = import เส้นประ = call
// ตัดโหนดที่ไม่มีเส้นออกเพื่อให้อ่านได้ (ponytail: ไม่มี UI ค้น/กรอง มากกว่าคลิกดูชื่อ)
func (s *Store) WriteGraphHTML(g Graph) (string, error) {
	type node struct {
		ID      string `json:"id"`
		Group   string `json:"group"`
		In      int    `json:"in"`
		Lang    string `json:"lang"`
		Symbols string `json:"symbols"`
	}
	type link struct {
		Source string `json:"source"`
		Target string `json:"target"`
		Kind   string `json:"kind"`
	}
	var nodes []node
	for _, n := range g.Nodes {
		if n.In == 0 && n.Out == 0 {
			continue
		}
		parts := strings.Split(n.Path, "/")
		group := parts[0]
		if len(parts) > 2 {
			group = parts[0] + "/" + parts[1]
		}
		nodes = append(nodes, node{ID: n.Path, Group: group, In: n.In, Lang: n.Lang, Symbols: strings.Join(head(n.Symbols, 6), ", ")})
	}
	var links []link
	for _, e := range g.Edges {
		links = append(links, link{e.From, e.To, e.Kind})
	}
	data, _ := json.Marshal(map[string]any{"nodes": nodes, "links": links, "engine": g.Engine, "built": g.BuiltAt, "root": g.Root})
	html := strings.Replace(graphTemplate, "/*DATA*/", string(data), 1)
	p := filepath.Join(s.Dir, "graph.html")
	return p, os.WriteFile(p, []byte(html), 0o644)
}

const graphTemplate = `<!doctype html><meta charset="utf-8"><title>blm graph</title>
<style>
html,body{margin:0;height:100%;background:#0f1115;color:#e6e6e6;font:13px ui-monospace,Menlo,monospace}
#bar{position:fixed;top:0;left:0;right:0;padding:8px 12px;background:#161a22;border-bottom:1px solid #2a2f3a;display:flex;gap:16px;align-items:center;z-index:2}
#bar b{color:#6fdc8c}#info{position:fixed;bottom:0;left:0;right:0;padding:8px 12px;background:#161a22;border-top:1px solid #2a2f3a;white-space:pre-wrap;max-height:30%;overflow:auto}
svg{width:100%;height:100%}.link{stroke:#556;stroke-opacity:.5}.link.call{stroke-dasharray:3 3;stroke:#8a7}
text{fill:#cfd3dc;font-size:10px;pointer-events:none}input{background:#0f1115;color:#e6e6e6;border:1px solid #2a2f3a;padding:4px 6px;width:260px}
</style>
<div id="bar"><b>blm graph</b><span id="meta"></span><input id="q" placeholder="filter: path or symbol…"><span id="legend">solid = import · dashed = call · size = in-degree · color = folder</span></div>
<svg></svg><div id="info">click a node…</div>
<script src="https://cdn.jsdelivr.net/npm/d3@7/dist/d3.min.js"></script>
<script>
const D=/*DATA*/;
document.getElementById('meta').textContent=D.root+' · '+D.engine+' · '+D.nodes.length+' nodes · '+D.links.length+' edges · '+D.built;
const svg=d3.select('svg'),W=innerWidth,H=innerHeight;const g=svg.append('g');
svg.call(d3.zoom().scaleExtent([0.1,6]).on('zoom',e=>g.attr('transform',e.transform)));
const color=d3.scaleOrdinal(d3.schemeTableau10);
const sim=d3.forceSimulation(D.nodes).force('link',d3.forceLink(D.links).id(d=>d.id).distance(40).strength(.4))
 .force('charge',d3.forceManyBody().strength(-60)).force('center',d3.forceCenter(W/2,H/2)).force('collide',d3.forceCollide(d=>6+Math.sqrt(d.in)*2));
const link=g.append('g').selectAll('line').data(D.links).join('line').attr('class',d=>'link '+d.kind);
const node=g.append('g').selectAll('circle').data(D.nodes).join('circle').attr('r',d=>4+Math.sqrt(d.in)*2).attr('fill',d=>color(d.group))
 .call(d3.drag().on('start',(e,d)=>{d.fx=d.x;d.fy=d.y}).on('drag',(e,d)=>{d.fx=e.x;d.fy=e.y}).on('end',(e,d)=>{d.fx=null;d.fy=null}))
 .on('click',(e,d)=>{const ins=D.links.filter(l=>l.target.id===d.id).map(l=>l.source.id),outs=D.links.filter(l=>l.source.id===d.id).map(l=>l.target.id);
  document.getElementById('info').textContent=d.id+'\n  symbols: '+(d.symbols||'-')+'\n  used by ('+ins.length+'): '+ins.join(', ')+'\n  uses ('+outs.length+'): '+outs.join(', ')});
const label=g.append('g').selectAll('text').data(D.nodes.filter(d=>d.in>=3)).join('text').text(d=>d.id.split('/').pop());
sim.on('tick',()=>{link.attr('x1',d=>d.source.x).attr('y1',d=>d.source.y).attr('x2',d=>d.target.x).attr('y2',d=>d.target.y);
 node.attr('cx',d=>d.x).attr('cy',d=>d.y);label.attr('x',d=>d.x+6).attr('y',d=>d.y+3)});
document.getElementById('q').oninput=e=>{const q=e.target.value.toLowerCase();
 node.attr('opacity',d=>!q||d.id.toLowerCase().includes(q)||(d.symbols||'').toLowerCase().includes(q)?1:.08);
 link.attr('opacity',l=>!q||l.source.id.toLowerCase().includes(q)||l.target.id.toLowerCase().includes(q)?1:.05)};
</script>`
