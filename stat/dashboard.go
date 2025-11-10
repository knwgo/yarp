package stat

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"

	"github.com/knwgo/yarp/config"
)

func StartDashboard(dc *config.Dashboard) {
	if dc == nil {
		dc = &config.Dashboard{
			BindAddr:     "127.0.0.1",
			HttpUser:     "",
			HttpPassword: "",
		}
	}

	hf := dashboardHandler
	if dc.HttpPassword != "" && dc.HttpUser != "" {
		hf = basicAuth(dashboardHandler, dc.HttpUser, dc.HttpPassword)
	}

	http.HandleFunc("/", hf)
	http.HandleFunc("/api/stats", statsAPI)
	go func() {
		fmt.Printf("[dashboard] running at http://%s\n", dc.BindAddr)
		_ = http.ListenAndServe(dc.BindAddr, nil)
	}()
}

func basicAuth(next http.HandlerFunc, user, pwd string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		basicUser, basicPass, ok := r.BasicAuth()
		if !ok || basicUser != user || basicPass != pwd {
			w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("Unauthorized\n"))
			return
		}
		next(w, r)
	}
}

func statsAPI(w http.ResponseWriter, _ *http.Request) {
	snapshot := GlobalStats.Snapshot()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(snapshot)
}

func dashboardHandler(w http.ResponseWriter, _ *http.Request) {
	tpl := `
<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<title>YARP Dashboard</title>
<style>
body { font-family: sans-serif; margin: 40px; background: #fafafa; position: relative; }
#lastUpdated {
	position: fixed;
	top: 10px;
	right: 20px;
	background: #eee;
	padding: 5px 10px;
	border-radius: 4px;
	font-size: 14px;
	box-shadow: 0 0 3px rgba(0,0,0,0.2);
}
table { border-collapse: collapse; width: 100%; background: white; margin-top: 50px; }
th, td { border: 1px solid #ccc; padding: 8px; text-align: left; }
th.sortable { background: #eee; cursor: pointer; user-select: none; }
th.sortable:hover { background: #ddd; }
th.sorted-asc::after { content: " ↑"; }
th.sorted-desc::after { content: " ↓"; }
</style>
<script>
let currentSort = { key: null, asc: true };

function formatBytes(bytes) {
	const units = ['B','KB','MB','GB','TB'];
	let i = 0;
	let num = bytes;
	while (num >= 1024 && i < units.length - 1) {
		num /= 1024;
		i++;
	}
	return num.toFixed(2) + ' ' + units[i];
}

async function refresh() {
	let res = await fetch('/api/stats');
	let snapshot = await res.json();
	let data = Object.entries(snapshot.ruleStats).map(([rule, v]) => ({ rule, ...v }));

	// 排序逻辑
	if (currentSort.key) {
		data.sort((a, b) => {
			let av = a[currentSort.key];
			let bv = b[currentSort.key];
			if (typeof av === 'string') av = av.toLowerCase();
			if (typeof bv === 'string') bv = bv.toLowerCase();
			if (av < bv) return currentSort.asc ? -1 : 1;
			if (av > bv) return currentSort.asc ? 1 : -1;
			return 0;
		});
	}

	let html = '<table><tr>' +
		'<th class="sortable" data-key="rule" onclick="sortBy(this)">Rule</th>' +
		'<th class="sortable" data-key="ConnCount" onclick="sortBy(this)">Conn</th>' +
		'<th class="sortable" data-key="BytesIn" onclick="sortBy(this)">BytesIn</th>' +
		'<th class="sortable" data-key="BytesOut" onclick="sortBy(this)">BytesOut</th>' +
		'<th class="sortable" data-key="RateInKBps" onclick="sortBy(this)">RateIn(KB/s)</th>' +
		'<th class="sortable" data-key="RateOutKBps" onclick="sortBy(this)">RateOut(KB/s)</th>' +
		'</tr>';

	for (let v of data) {
		html += '<tr>' +
			'<td>' + v.rule + '</td>' +
			'<td>' + v.ConnCount + '</td>' +
			'<td>' + formatBytes(v.BytesIn) + '</td>' +
			'<td>' + formatBytes(v.BytesOut) + '</td>' +
			'<td>' + v.RateInKBps.toFixed(2) + '</td>' +
			'<td>' + v.RateOutKBps.toFixed(2) + '</td>' +
			'</tr>';
	}

	html += '</table>';
	document.getElementById('statsTable').innerHTML = html;

	// 设置列头箭头状态
	for (let th of document.querySelectorAll('th.sortable')) {
		th.classList.remove('sorted-asc', 'sorted-desc');
		if (th.dataset.key === currentSort.key) {
			th.classList.add(currentSort.asc ? 'sorted-asc' : 'sorted-desc');
		}
	}

	let t = new Date(snapshot.lastUpdateTime);
	document.getElementById('lastUpdated').innerText = 'Last Updated: ' + t.toLocaleString();
}

function sortBy(th) {
	const key = th.dataset.key;
	if (currentSort.key === key) {
		currentSort.asc = !currentSort.asc;
	} else {
		currentSort.key = key;
		currentSort.asc = true;
	}
	refresh();
}

setInterval(refresh, 1000);
window.onload = refresh;
</script>
</head>
<body>
<div id="lastUpdated">Loading...</div>
<div id="statsTable">Loading...</div>
</body>
</html>
`
	_ = template.Must(template.New("dash").Parse(tpl)).Execute(w, nil)
}
