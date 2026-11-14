# Baseline: idle, then list pages, then an SSE stream.
sleep 45; sample idle45
resetpeak
for s in added_on name size ratio tracker state; do
  for p in 0 1 2; do curl -s "$Q/api/instances/1/torrents?page=$p&limit=300&sort=$s&order=desc" -o /dev/null; done
done
curl -s "$Q/api/instances/1/torrents?limit=300&search=Harbor" -o /dev/null
sample listed
resetpeak
STREAMS=$(python3 -c 'import urllib.parse,json; print(urllib.parse.quote(json.dumps([{"key":"k1","instanceId":1,"page":0,"limit":300,"sort":"added_on","order":"desc"}])))')
timeout 40 curl -sN "$Q/api/stream?streams=$STREAMS" -o "$OUT/sse.txt"
sample sse40
sleep 60; sample idle-after
