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
#bar{position:fixed;top:0;left:0;right:0;padding:6px 12px;background:#161a22;border-bottom:1px solid #2a2f3a;display:flex;gap:10px;align-items:center;flex-wrap:wrap;z-index:2}
#bar b{color:#6fdc8c}#meta{color:#9aa}
#info{position:fixed;bottom:0;left:0;right:0;padding:8px 12px;background:#161a22;border-top:1px solid #2a2f3a;white-space:pre-wrap;max-height:30%;overflow:auto}
#legend{position:fixed;right:10px;top:48px;background:#161a22cc;border:1px solid #2a2f3a;padding:6px 8px;max-height:60%;overflow:auto;font-size:11px;z-index:2}
#legend div{display:flex;gap:6px;align-items:center;cursor:pointer;padding:1px 0}#legend i{display:inline-block;width:10px;height:10px;border-radius:50%}#legend .off{opacity:.35}
svg{width:100%;height:100%}.link{stroke:#556;stroke-opacity:.5}.link.call{stroke-dasharray:3 3;stroke:#8a7}
text{fill:#cfd3dc;font-size:10px;pointer-events:none}input{background:#0f1115;color:#e6e6e6;border:1px solid #2a2f3a;padding:4px 6px;width:220px}
button{background:#0f1115;color:#e6e6e6;border:1px solid #2a2f3a;padding:3px 8px;cursor:pointer}button.on{background:#2b6;color:#000;border-color:#2b6}
.hint{color:#889;font-size:11px}
</style>
<div id="bar"><b>blm graph</b><span id="meta"></span>
 <span>view:</span><button data-view="files" class="on">Files</button><button data-view="folders">Folders</button>
 <span>show:</span><button data-scope="all" class="on">All</button><button data-scope="code">Code</button><button data-scope="docs">Docs</button>
 <button id="focus">Focus: off</button><input id="q" placeholder="filter: path or symbol…">
 <span class="hint">solid = import · dashed = call · size = in-degree · color = folder · scroll = zoom · click = details · legend toggles folders</span></div>
<div id="legend"></div>
<svg></svg><div id="info">click a node…</div>
<script src="https://cdn.jsdelivr.net/npm/d3@7/dist/d3.min.js"></script>
<script>
const D=/*DATA*/;
const isDoc=n=>n.lang==='md';
document.getElementById('meta').textContent=D.root.split('/').pop()+' · '+D.engine+' · '+D.nodes.length+' nodes · '+D.links.length+' edges · '+D.built;
const color=d3.scaleOrdinal(d3.schemeTableau10);
const groups=[...new Set(D.nodes.map(n=>n.group))].sort();
const hidden=new Set();let view='files',scope='all',focus=false,focusId=null,q='';
// legend: คลิกซ่อน/แสดงโฟลเดอร์
const leg=d3.select('#legend');
leg.selectAll('div').data(groups).join('div').html(g=>'<i style="background:'+color(g)+'"></i>'+g)
 .on('click',(e,g)=>{hidden.has(g)?hidden.delete(g):hidden.add(g);leg.selectAll('div').classed('off',g=>hidden.has(g));render()});
// folder view: ยุบโหนดเป็นโฟลเดอร์ เส้น = จำนวนเส้นระหว่างโฟลเดอร์
function folderData(){const fn={},fl={};
 D.nodes.forEach(n=>{const f=fn[n.group]||(fn[n.group]={id:n.group,group:n.group,in:0,files:0,lang:n.lang,symbols:''});f.files++;f.in+=n.in});
 D.links.forEach(l=>{const a=(l.source.id||l.source),b=(l.target.id||l.target);const ga=D.byId[a].group,gb=D.byId[b].group;if(ga===gb)return;const k=ga+'>'+gb;(fl[k]||(fl[k]={source:ga,target:gb,kind:l.kind,n:0})).n++});
 return {nodes:Object.values(fn),links:Object.values(fl)}}
D.byId={};D.nodes.forEach(n=>D.byId[n.id]=n);
const svg=d3.select('svg'),W=innerWidth,H=innerHeight;const g=svg.append('g');
const zoom=d3.zoom().scaleExtent([0.1,8]).on('zoom',e=>{g.attr('transform',e.transform);labels.style('display',e.transform.k>1.6?null:'none')});
svg.call(zoom);
let sim,links,nodes,labels=g.append('g');
function visible(n){if(hidden.has(n.group))return false;if(scope==='code'&&isDoc(n))return false;if(scope==='docs'&&!isDoc(n))return false;return true}
function render(){
 const base=view==='folders'?folderData():{nodes:D.nodes.map(n=>Object.assign({},n)),links:D.links.map(l=>({source:l.source.id||l.source,target:l.target.id||l.target,kind:l.kind,n:1}))};
 let ns=base.nodes.filter(visible);const ids=new Set(ns.map(n=>n.id));let ls=base.links.filter(l=>ids.has(l.source)&&ids.has(l.target));
 if(focus&&focusId&&view==='files'){const keep=new Set([focusId]);ls.forEach(l=>{if(l.source===focusId)keep.add(l.target);if(l.target===focusId)keep.add(l.source)});
  const k2=new Set(keep);ls.forEach(l=>{if(keep.has(l.source))k2.add(l.target);if(keep.has(l.target))k2.add(l.source)});ns=ns.filter(n=>k2.has(n.id));const ids2=new Set(ns.map(n=>n.id));ls=ls.filter(l=>ids2.has(l.source)&&ids2.has(l.target))}
 g.selectAll('*').remove();
 sim&&sim.stop();
 sim=d3.forceSimulation(ns).force('link',d3.forceLink(ls).id(d=>d.id).distance(view==='folders'?120:40).strength(.4))
  .force('charge',d3.forceManyBody().strength(view==='folders'?-600:-60)).force('center',d3.forceCenter(W/2,H/2)).force('collide',d3.forceCollide(d=>8+Math.sqrt(d.in)*2));
 links=g.append('g').selectAll('line').data(ls).join('line').attr('class',d=>'link '+d.kind).attr('stroke-width',d=>Math.min(1+Math.log2(d.n||1),6));
 nodes=g.append('g').selectAll('circle').data(ns).join('circle').attr('r',d=>4+Math.sqrt(d.in)*2).attr('fill',d=>color(d.group))
  .call(d3.drag().on('start',(e,d)=>{d.fx=d.x;d.fy=d.y}).on('drag',(e,d)=>{d.fx=e.x;d.fy=e.y}).on('end',(e,d)=>{d.fx=null;d.fy=null}))
  .on('click',(e,d)=>{const ins=ls.filter(l=>l.target.id===d.id).map(l=>l.source.id+(l.n>1?' ×'+l.n:'')),outs=ls.filter(l=>l.source.id===d.id).map(l=>l.target.id+(l.n>1?' ×'+l.n:''));
   document.getElementById('info').textContent=d.id+(d.files?'  ('+d.files+' files)':'')+'\n  symbols: '+(d.symbols||'-')+'\n  used by ('+ins.length+'): '+ins.join(', ')+'\n  uses ('+outs.length+'): '+outs.join(', ');
   if(focus){focusId=d.id;render()}})
  .on('mouseover',(e,d)=>{hover.text(d.id).attr('x',d.x+8).attr('y',d.y-6).style('display',null)}).on('mouseout',()=>hover.style('display','none'));
 labels=g.append('g').selectAll('text').data(ns.filter(d=>view==='folders'||d.in>=3)).join('text').text(d=>view==='folders'?d.id:d.id.split('/').pop());
 if(view==='files')labels.style('display',d3.zoomTransform(svg.node()).k>1.6?null:'none');
 const hover=g.append('text').attr('font-size',12).attr('fill','#fff').style('display','none');
 sim.on('tick',()=>{links.attr('x1',d=>d.source.x).attr('y1',d=>d.source.y).attr('x2',d=>d.target.x).attr('y2',d=>d.target.y);
  nodes.attr('cx',d=>d.x).attr('cy',d=>d.y);labels.attr('x',d=>d.x+6).attr('y',d=>d.y+3)});
 applyFilter()}
function applyFilter(){nodes.attr('opacity',d=>!q||d.id.toLowerCase().includes(q)||(d.symbols||'').toLowerCase().includes(q)?1:.08);
 links.attr('opacity',l=>!q||l.source.id.toLowerCase().includes(q)||l.target.id.toLowerCase().includes(q)?1:.05)}
document.querySelectorAll('[data-view]').forEach(b=>b.onclick=()=>{view=b.dataset.view;document.querySelectorAll('[data-view]').forEach(x=>x.classList.toggle('on',x===b));render()});
document.querySelectorAll('[data-scope]').forEach(b=>b.onclick=()=>{scope=b.dataset.scope;document.querySelectorAll('[data-scope]').forEach(x=>x.classList.toggle('on',x===b));render()});
document.getElementById('focus').onclick=e=>{focus=!focus;if(!focus)focusId=null;e.target.textContent='Focus: '+(focus?'on (click a node)':'off');e.target.classList.toggle('on',focus);render()};
document.getElementById('q').oninput=e=>{q=e.target.value.toLowerCase();applyFilter()};
render();
</script>`
