package protocol

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
)

func init() {
	StartDashboard("127.0.0.1:8080")
}

func StartDashboard(addr string) {
	http.HandleFunc("/", dashboardHandler)
	http.HandleFunc("/api/stats", statsAPI)
	go func() {
		fmt.Printf("[dashboard] running at http://%s\n", addr)
		_ = http.ListenAndServe(addr, nil)
	}()
}

// /api/stats 返回 Snapshot JSON
func statsAPI(w http.ResponseWriter, r *http.Request) {
	snapshot := GlobalStats.Snapshot()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(snapshot)
}

func dashboardHandler(w http.ResponseWriter, r *http.Request) {
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
th { background: #eee; }
</style>
<script>
// 格式化字节为 B/KB/MB/GB
function formatBytes(bytes) {
    if (bytes < 1024) return bytes + " B";
    let k = bytes / 1024;
    if (k < 1024) return k.toFixed(2) + " KB";
    let m = k / 1024;
    if (m < 1024) return m.toFixed(2) + " MB";
    return (m/1024).toFixed(2) + " GB";
}

// 格式化速率 KB/s -> KB/s 或 MB/s
function formatRate(kbps) {
    if (kbps < 1024) return kbps.toFixed(2) + " KB/s";
    return (kbps/1024).toFixed(2) + " MB/s";
}

async function refresh(){
    let res = await fetch('/api/stats');
    let snapshot = await res.json();
    let data = snapshot.ruleStats;

    let html = '<table><tr><th>Rule</th><th>Conn</th><th>BytesIn</th><th>BytesOut</th><th>RateIn</th><th>RateOut</th></tr>';
    for(let k in data){
        let v = data[k];
        html += '<tr>'+
                '<td>'+k+'</td>'+
                '<td>'+v.ConnCount+'</td>'+
                '<td>'+formatBytes(v.BytesIn)+'</td>'+
                '<td>'+formatBytes(v.BytesOut)+'</td>'+
                '<td>'+formatRate(v.RateInKBps)+'</td>'+
                '<td>'+formatRate(v.RateOutKBps)+'</td>'+
                '</tr>';
    }
    html += '</table>';
    document.getElementById('statsTable').innerHTML = html;

    let t = new Date(snapshot.lastUpdateTime);
    document.getElementById('lastUpdated').innerText = 'Last Updated: ' + t.toLocaleString();
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
