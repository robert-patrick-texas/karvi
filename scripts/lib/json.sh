# JSON for the scripts: one parser, read by path. Source it:
#
#   . "$ROOT/scripts/lib/json.sh"
#
# A script asserts on a JSON file's content through these functions and
# never through grep on the file's text, which holds only for one layout:
# the .json files became indented and every text match on `"key":value`
# broke at once. The functions read a value; the
# assertion, the row's name, and the failure message stay in the script.
#
# The parser is python3, a prerequisite of BUILD-HOWTO.md §1 that the
# verifiers already need; nothing else is required. For a .jsonl file, read
# one line into the function's standard input (FILE is "-").
#
# PATH is dot-separated keys from the top: `device_counts.incomplete`. A
# number indexes an array (`terminal_causes.0`); `*` takes every element of
# an array or every value of an object, one result per line
# (`ssh_transports.*.id`). Keys holding a dot cannot be named; no file the
# scripts read has one.

command -v python3 >/dev/null 2>&1 || { echo "scripts/lib/json.sh: python3 is required (BUILD-HOWTO.md §1)" >&2; exit 2; }

# json_get FILE PATH: print the value at PATH. A string prints as itself,
# true/false/null and numbers as JSON writes them, an array or object as
# compact JSON. Exit 1 if PATH is absent (with `*`, if nothing matched),
# exit 2 if FILE is not JSON or cannot be read; the reason goes to stderr.
json_get() {
  python3 -c '
import json, sys
source, path = sys.argv[1], sys.argv[2]
try:
    with (sys.stdin if source == "-" else open(source, encoding="utf-8")) as f:
        document = json.load(f)
except (OSError, ValueError) as e:
    sys.stderr.write("json_get: %s: %s\n" % (source, e)); sys.exit(2)

def walk(node, keys):
    if not keys:
        yield node; return
    key, rest = keys[0], keys[1:]
    if key == "*":
        for child in (node.values() if isinstance(node, dict) else node if isinstance(node, list) else ()):
            yield from walk(child, rest)
    elif isinstance(node, dict) and key in node:
        yield from walk(node[key], rest)
    elif isinstance(node, list) and key.isdigit() and int(key) < len(node):
        yield from walk(node[int(key)], rest)

found = list(walk(document, path.split(".") if path else []))
if not found:
    sys.stderr.write("json_get: %s: no %s\n" % (source, path)); sys.exit(1)
for value in found:
    print(value if isinstance(value, str) else json.dumps(value, separators=(",", ":"), ensure_ascii=False))
' "$1" "$2"
}

# json_has FILE PATH: true if PATH is present, whatever its value (null
# included). Silent; exit 2 still means FILE is not JSON.
json_has() {
  json_get "$1" "$2" >/dev/null 2>&1
}

# json_is FILE PATH WANT: true if the value at PATH prints as WANT. The
# common assertion, so that a script does not spell the comparison each time.
json_is() {
  [ "$(json_get "$1" "$2" 2>/dev/null)" = "$3" ]
}
