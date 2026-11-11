# Sync cost: one live-update stream keeps qui syncing for 60 s. Reports the CPU
# time of qui and the bytes it allocated, each divided by the number of syncs.
cpu() { awk '{print $14+$15}' /proc/$QUI/stat; }
syncs() { curl -s 127.0.0.1:18080/_counts | python3 -c 'import sys,json; print(json.load(sys.stdin).get("/api/v2/sync/maindata",0))'; }
alloc() { curl -fs "127.0.0.1:16060/debug/pprof/heap?debug=1" | awk '/^# TotalAlloc/{print $4}'; }
sleep 20; sample idle20
q=$(python3 -c 'import urllib.parse,json; print(urllib.parse.quote(json.dumps([{"key":"k1","instanceId":1,"page":0,"limit":300,"sort":"added_on","order":"desc"}])))')
stream 75 "$q"
sleep 10
resetpeak; c0=$(cpu); s0=$(syncs); a0=$(alloc)
sleep 60
c1=$(cpu); s1=$(syncs); a1=$(alloc)
sample stream60
if ! [ "$s1" -gt "$s0" ] 2>/dev/null; then echo "synccost NO-SYNCS" | tee -a "$OUT/samples.txt"; exit 1; fi
if ! cost=$(python3 -c 'import sys; c,s,a=[int(sys.argv[i+3])-int(sys.argv[i]) for i in (1,2,3)]; print("synccost syncs=%d cpu_ms_per_sync=%.1f alloc_mb_per_sync=%.2f" % (s, c*10.0/s, a/1048576/s))' "$c0" "$s0" "$a0" "$c1" "$s1" "$a1"); then echo "synccost FAILED" | tee -a "$OUT/samples.txt"; exit 1; fi
echo "$cost" | tee -a "$OUT/samples.txt"
sleep 15; sample idle-after
checkstreams
