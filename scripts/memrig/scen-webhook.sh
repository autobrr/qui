# Webhook check: the call autobrr makes for each announce, 1, 8 and 32 at once.
hook() { # hook <clients> <rounds>
  for r in $(seq $2); do
    for c in $(seq $1); do
      curl -s -o "$OUT/hook-last.json" -w '%{http_code}\n' "${H[@]}" -X POST "$Q/api/cross-seed/webhook/check" \
        -d "{\"torrentName\":\"Amber.Signal.Vale.$((r * 100 + c)).S0$((c % 9 + 1))E$((10 + c % 14)).1080p.WEB-DL.DDP5.1.H.264-GRP$c\",\"size\":$((1000000000 + c))}" >> "$OUT/hook-codes.txt" &
    done
    wait $(jobs -p | grep -v -e "^$QUI\$" -e "^$STUB\$") 2>/dev/null
  done
}
sleep 20; sample idle20
for k in ${KS:-1 8 32}; do resetpeak; hook $k 6; sample "hook-x$k"; done
sort "$OUT/hook-codes.txt" | uniq -c | tee -a "$OUT/samples.txt"
sleep 30; sample idle-after
