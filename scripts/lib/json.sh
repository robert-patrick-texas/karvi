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

# The path walk both readers share: walk(node, keys) yields every value at
# the keys, `*` taking each element or value; text(value) prints a string
# as itself and anything else as compact JSON.
JSON_PY_WALK='
import json, sys

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

def text(value):
    return value if isinstance(value, str) else json.dumps(value, separators=(",", ":"), ensure_ascii=False)
'

# json_get FILE PATH: print the value at PATH. A string prints as itself,
# true/false/null and numbers as JSON writes them, an array or object as
# compact JSON. Exit 1 if PATH is absent (with `*`, if nothing matched),
# exit 2 if FILE is not JSON or cannot be read; the reason goes to stderr.
json_get() {
  python3 -c "$JSON_PY_WALK"'
source, path = sys.argv[1], sys.argv[2]
try:
    with (sys.stdin if source == "-" else open(source, encoding="utf-8")) as f:
        document = json.load(f)
except (OSError, ValueError) as e:
    sys.stderr.write("json_get: %s: %s\n" % (source, e)); sys.exit(2)

found = list(walk(document, path.split(".") if path else []))
if not found:
    sys.stderr.write("json_get: %s: no %s\n" % (source, path)); sys.exit(1)
for value in found:
    print(text(value))
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

# jsonl_records FILE PATH...: one line per command record of a .jsonl
# stream (a line holding record_id; the job summary and any other line are
# skipped), the values at the PATHs tab-separated in the order given. An
# absent value is empty; the values `*` matches are joined by commas; a
# string holding a tab or a newline prints as JSON writes it, quoted, so a
# record stays one line and its fields stay apart. The assertion is the
# script's, on this output: `| grep -cx succeeded`, `| head -1`,
# `| paste -sd, -`. Exit 2 if FILE cannot be read or a line is not JSON.
jsonl_records() {
  python3 -c "$JSON_PY_WALK"'
source, paths = sys.argv[1], sys.argv[2:]

def field(node, path):
    out = []
    for value in walk(node, path.split(".")):
        v = text(value)
        out.append(json.dumps(v, ensure_ascii=False) if "\t" in v or "\n" in v or "\r" in v else v)
    return ",".join(out)

try:
    with (sys.stdin if source == "-" else open(source, encoding="utf-8")) as f:
        for number, line in enumerate(f, 1):
            if not line.strip():
                continue
            try:
                record = json.loads(line)
            except ValueError as e:
                sys.stderr.write("jsonl_records: %s:%d: %s\n" % (source, number, e)); sys.exit(2)
            if isinstance(record, dict) and "record_id" in record:
                print("\t".join(field(record, p) for p in paths))
except OSError as e:
    sys.stderr.write("jsonl_records: %s: %s\n" % (source, e)); sys.exit(2)
' "$@"
}
