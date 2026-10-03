# Display configuration

Display settings affect human-facing terminal projections only. JSONL, JSON
records, audit events, logs, manifests, summaries, and persisted scoreboards do
not receive display ANSI sequences or border text.

## Defaults

```toml
[display]
theme = "dark"
color = "auto"
timestamp = "hh:mm:ss yyyy-mm-dd"

[display.command]
header = "! <target> [<address>] platform=<platform> user=<user> backend=<auth-backend> transport=<transport>"
footer = "! exit=<exit-code> elapsed=<elapsed> artifacts=<artifacts>"
border = "\n"
dynamic-border-length = 72
last-border = false
echo = false

[display.run]
header = "! <target> [<address>] platform=<platform> user=<user> backend=<auth-backend> transport=<transport>"
footer = "! exit=<exit-code> elapsed=<elapsed> artifacts=<artifacts>"
border = "\n"
dynamic-border-length = 72
last-border = false
echo = false

[display.record]
header = "! transcript=<transcript>"
footer = "! transcript=<transcript>"

[display.collection]
footer = "! collection=<collection> replaced=<replaced> kept=<kept>"

[display.json]
indent = 2
```

## The footer

`display.command.footer` ends a `command`'s display. `display.run.footer`
ends a run's text display on every path, the daemon follow, `--no-daemon`,
`job follow`, and a finished job's replay, with the job's exit, elapsed
time, and job folder from its summary; nothing ends a run on standard
error. Under `--format jsonl` the summary
document is the stream's last line instead, and `--format json` stays the
records' array. An empty template disables either footer. With `cmd
--nof`, or with all eight
`output.files` keys false (`docs/OPERATIONS.md` "The job's output
files"), `<artifacts>` is `none`.

## The collection line

`display.collection.footer` follows the footer of a job with a collection
(`crun`, or `run` or `command` given `--cd`, `docs/COLLECTION.md`), on
standard output directly after it, on every text path: `--no-daemon`, the
daemon follow, `job follow`, and a finished job's replay. The default,
`"! collection=<collection> replaced=<replaced> kept=<kept>"`, renders
`<collection>` (the absolute collection directory), `<replaced>`, and
`<kept>` (the devices whose files were replaced and kept) in the `value`
role, the literals in the `label` role, as the footer is rendered:

```text
! exit=101 elapsed=900ms artifacts=/srv/karvi/jobs/261003/261003-103623-00
! collection=/srv/configs replaced=1 kept=1
```

Under `--format json` and `--format jsonl` nothing is printed; the jsonl
summary document and `summary.json` carry the `collection` block. An empty
template prints no line, `--quiet` suppresses it, and a template that does
not render refuses the configuration (`config_display_template_invalid`).
It replaced the plain `crun JOB-ID exit=… collection=…` line on standard
error.

## The ping line

`display.ping.header` is the ICMP gate's line for each gated device
(`network.ping-targets`, `--ping`), printed before the device's header in
text output and rendered as the headers are: the literals in the `label`
role, `<target>` and `<address>` in theirs, `<rtt1>` and `<rtt2>` (each
probe's round-trip time, `timeout`, or `error`) in the `value` role, and
`<result>` bold in the `success` role for `proceeding` or the `error` role
for `skipped`. The default is
`"! <target> [<address>] ping(1) <rtt1>, ping(2) <rtt2>, <result>"`. An
empty template prints no line; `--quiet` suppresses it; `--debug` adds the
error details and the packet-loss notice after it. `login` prints the same
line on standard error. The template replaced a boolean switch.

## The recorded login's lines

`display.record.header` and `display.record.footer` name a recorded
login's transcript (`login --record`, `docs/LOGIN-TRANSCRIPTS.md`): the
header before the session's first line, the footer as its last, after the
device's last output and for a session that failed as well. Both default
to `"! transcript=<transcript>"`, so the two lines match; `<transcript>` is
the file's path, in the `value` role with the literals in the `label`
role, as the login's header is rendered. The templates may also use
`<timestamp>`, `<target>`, `<platform>`, and `<transport>`, and the footer
`<exit-status>`, `<exit-code>`, and `<elapsed>`. They go to standard error,
on the terminal and never into the transcript; an empty template prints no
line; `--quiet` suppresses both; a template that does not render refuses
the login before a transcript is created (`config_display_template_invalid`).

## Borders as separators

A border separates adjacent command records. With the default `"\n"`, two
records have one blank line between them. No border follows the last record
unless the applicable `last-border` is true.

`--border` replaces the configured border with dashes. The length is the visible
width of the displayed header; with no header, the mode-specific
`dynamic-border-length` is used. The line is capped at terminal width.

`--noborder` suppresses configured and dynamic borders. Combining `--border`
and `--noborder` is a usage error. A configured empty string disables the
static separator without requiring a CLI option.

## Width and colors

Visible width excludes ANSI escape sequences. Overlong headers and footers are
split only between complete template elements, with a trailing artifacts
element moved to a new line first. Only border lines may be cropped. Device
output and prompt/command echo are preserved byte-for-byte after the existing
ANSI policy.

Dark-theme defaults render target bold yellow, address bold magenta (as the
light theme does), labels and brackets blue, values white, success green, and
borders gray. Timestamp and dynamic-border roles
are independently configurable.

`karvi config colors` prints the colour test:
every `display.colors.*` role under the configured theme first and then
the other, each line the key rendered as a display line renders that role
(the target and the address bold), the colour it resolves to, `default`
or `configured`, and the escape a terminal receives; a configured override
holds under both themes. Colour follows the rule above, so on a pipe the
words print alone and the escape column still says what a terminal gets;
`--set display.color=always` forces it.

`--help` follows the same rule: under
`display.color` and `display.theme`, on a terminal, the title line's tag
(`- run the fleet, gather the output`) takes the `label` colour, a
section heading is bold, an option's words take the `accent` colour, and
the action words of
the `Usage:` lines (`run` in `karvi run [target inputs]`, `daemon start`,
`setup shared` after a plain `sudo`, `<command>` in the top text) take
the `success` colour, green under both
themes and `display.colors.success` overriding; a `--config` or `--set`
before the `--help` is read; `--ansi strip` turns it off; a configuration
that does not load gives plain help. The words are never changed, so
`--help | grep` reads as before; the layout adds a blank line around a
paragraph inside a section and after an entry of three lines or more.

## Echo

`display.command.echo`, `display.run.echo`, and `--echo` show the effective
prompt plus exact command before text output. This includes device-reported
command errors. Structured records distinguish observed and inferred prompts.

## Output formats

- `text`: human projection with optional header, echo, separator, and footer.
- `jsonl`: one compact record per line for programs.
- `json`: one indented JSON array for human inspection.

`display.json.indent` accepts 1 through 8 and affects only `json`.
