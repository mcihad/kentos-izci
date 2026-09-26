# Renders `make help` from annotations that sit directly above each target:
#
#   #? description line (repeatable)
#   #= VAR  what the variable does (repeatable)
#   #> example invocation (repeatable)
#   target: deps ## one-line summary
#
# `make help` lists every target that has a `##` summary; `make help T=name`
# prints the full description, variables and examples of one target.
# `make help-check` (mode=check) fails when a documented target lacks a
# description or an example.

function flush() { desc = ""; vars = ""; ex = "" }
function indent(s,    out, n, i, lines) {
  n = split(s, lines, "\n"); out = ""
  for (i = 1; i <= n; i++) if (lines[i] != "") out = out "    " lines[i] "\n"
  return out
}

BEGIN {
  flush(); count = 0; bad = 0
  if (mode == "" && target == "") {
    print "Usage: make <target> [VAR=value]      Details: make help T=<target>\n"
  }
}

/^#\?/ { line = $0; sub(/^#\? ?/, "", line); desc = desc line "\n"; next }
/^#=/  { line = $0; sub(/^#= ?/, "", line); vars = vars line "\n"; next }
/^#>/  { line = $0; sub(/^#> ?/, "", line); ex = ex line "\n"; next }

/^[a-zA-Z0-9_.-]+:[^=]*##/ {
  name = $0; sub(/:.*/, "", name)
  short = $0; sub(/.*## */, "", short)
  count++
  if (mode == "check") {
    if (desc == "" || ex == "") { printf "missing %s%s for target %s\n", (desc == "" ? "#? " : ""), (ex == "" ? "#> " : ""), name > "/dev/stderr"; bad++ }
  } else if (target == "") {
    printf "  \033[36m%-18s\033[0m %s\n", name, short
  } else if (target == name) {
    found = 1
    printf "\033[1m%s\033[0m — %s\n", name, short
    if (desc != "") printf "\n%s", indent(desc)
    if (vars != "") printf "\nVariables:\n%s", indent(vars)
    if (ex != "")   printf "\nExamples:\n%s", indent(ex)
  }
  flush(); next
}

/^[^#]/ { flush() }

END {
  if (mode == "check") {
    if (count < 10) { printf "help-check scanned only %d targets: the scan itself looks broken\n", count > "/dev/stderr"; exit 1 }
    if (bad > 0) exit 1
    printf "help-check: %d/%d targets documented\n", count, count
  } else if (target != "" && !found) {
    printf "unknown target: %s\n", target > "/dev/stderr"; exit 1
  }
}
