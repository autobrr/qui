# SSE: 1, 8 and 24 live-update subscriptions, each a distinct view of the list.
SORTS=(added_on name size ratio tracker state category tags uploaded downloaded num_seeds num_leechs eta progress dlspeed upspeed)
streams() { # streams <count> <seconds>
  for c in $(seq $1); do
    q=$(python3 -c 'import urllib.parse,json,sys; c=int(sys.argv[1]); print(urllib.parse.quote(json.dumps([{"key":"k%d"%c,"instanceId":1,"page":c,"limit":300,"sort":sys.argv[2],"order":"desc" if c%2 else "asc"}])))' $c ${SORTS[$((c % ${#SORTS[@]}))]})
    stream $2 "$q"
  done
  sleep $2; sleep 2
}
sleep 20; sample idle20
for k in 1 8 24; do resetpeak; streams $k 40; sample "sse-x$k"; done
sleep 30; sample idle-after
checkstreams
