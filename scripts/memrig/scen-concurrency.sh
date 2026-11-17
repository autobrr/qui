# Concurrency: the same list request sent by 1, 4, 16 and 32 clients at once,
# each with its own sort so no response cache can serve two of them.
SORTS=(added_on name size ratio tracker state category tags uploaded downloaded num_seeds num_leechs eta progress dlspeed upspeed)
burst() { # burst <clients> <rounds>
  for r in $(seq $2); do
    for c in $(seq $1); do
      s=${SORTS[$(( (c + r) % ${#SORTS[@]} ))]}
      curl -s "$Q/api/instances/1/torrents?page=$((c % 5))&limit=300&sort=$s&order=$([ $((c % 2)) = 0 ] && echo asc || echo desc)&search=$( [ $((c % 3)) = 0 ] && echo Harbor )" -o /dev/null -w '%{http_code}\n' >> "$OUT/list-codes.txt" &
    done
    wait $(jobs -p | grep -v -e "^$QUI\$" -e "^$STUB\$") 2>/dev/null
  done
}
sleep 20; sample idle20
for k in 1 4 16 32; do resetpeak; burst $k 6; sample "list-x$k"; done
sort "$OUT/list-codes.txt" | uniq -c | tee -a "$OUT/samples.txt"
sleep 30; sample idle-after
if [ "$(sort -u "$OUT/list-codes.txt")" != 200 ]; then echo "LIST-FAILED" | tee -a "$OUT/samples.txt"; exit 1; fi
