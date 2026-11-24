#!/usr/bin/env bash
# Model-check every generated spec under both guards and summarise.
#
#   ./run_tlc.sh path/to/tla2tools.jar [dir]     # dir defaults to .
#
# Writes <dir>/results/<spec>_<guard>.out and prints one line per run:
#   spec variant(guard[_serial]) verdict distinct-states
set -u
JAR=$1
DIR=${2:-.}
mkdir -p "$DIR/results"
META=$(mktemp -d)
for spec in "$DIR"/DW_*.tla; do
  case "$spec" in *_TTrace_*) continue;; esac
  base=$(basename "$spec" .tla)
  for cfg in "$DIR/${base}"_2009*.cfg; do
    variant=$(basename "$cfg" .cfg); variant=${variant#"${base}_"}
    out="$DIR/results/${base}_${variant}.out"
    (cd "$DIR" && timeout 300 java -XX:+UseParallelGC -cp "$JAR" tlc2.TLC -workers 1 -noGenerateSpecTE \
        -config "$(basename "$cfg")" "$(basename "$spec")" -metadir "$META/$base-$variant") > "$out" 2>&1
    if grep -q "No error has been found" "$out"; then v=OK
    elif grep -q "is violated" "$out"; then v=VIOLATION:$(grep -o "Invariant [A-Za-z]* is violated" "$out" | head -1 | cut -d' ' -f2)
    elif grep -q "Deadlock reached" "$out"; then v=DEADLOCK
    else v=INCOMPLETE
    fi
    states=$(grep -o "[0-9,]* distinct states found" "$out" | tail -1 | cut -d' ' -f1)
    echo "$base $variant $v $states"
  done
done
rm -rf "$META"
