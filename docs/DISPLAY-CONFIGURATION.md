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
gather = false
prefix = false

[display.run]
header = "! <target> [<address>] platform=<platform> user=<user> backend=<auth-backend> transport=<transport>"
footer = "! exit=<exit-code> elapsed=<elapsed> artifacts=<artifacts>"
border = "\n"
dynamic-border-length = 72
last-border = false
echo = false
gather = false
prefix = false

[display.record]
header = "! transcript=<transcript>"
footer = "! transcript=<transcript>"

[display.collection]
footer = "! collection=<collection> replace=<replace> keep=<keep>"

[display.gather]
header = "! <targets> (<count>)"

[display.json]
indent = 2
```

## The footer

`display.command.footer` ends a `command`'s display. `display.run.footer` ends a
run's text display on every path, the daemon follow, `--no-daemon`, `job
follow`, and a finished job's replay, with the job's exit, elapsed time, and job
folder from its summary; nothing ends a run on standard error. Under `--format
jsonl` the summary document is the stream's last line instead, and `--format
json` stays the records' array. An empty template disables either footer. With
`cmd --nof`, or with `output.persist-command = false`
([`docs/OPERATIONS.md`](OPERATIONS.md) ["The job's output
files"](OPERATIONS.md#the-jobs-output-files)), `<artifacts>` is `none`.

## The collection line

`display.collection.footer` follows the footer of a job with a collection
(`crun`, or `run` or `command` given `--cd`,
[`docs/COLLECTION.md`](COLLECTION.md)), on standard output directly after it, on
every text path: `--no-daemon`, the daemon follow, `job follow`, and a finished
job's replay. The default, `"! collection=<collection> replace=<replace>
keep=<keep>"`, renders `<collection>` (the absolute collection directory),
`<replace>`, and `<keep>` (the devices whose files were replaced and kept) in
the `value` role, the literals in the `label` role, as the footer is rendered:

```text
! exit=101 elapsed=900ms artifacts=/srv/karvi/jobs/261003/261003-103623-00
! collection=/srv/configs replace=1 keep=1
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
`<result>` bold in the `success` role for `proceed` or the `error` role for
`skip`. The default is
`"! <target> [<address>] ping(1) <rtt1>, ping(2) <rtt2>, <result>"`. An
empty template prints no line; `--quiet` suppresses it; `--debug` adds the
error details and the packet-loss notice after it. `login` prints the same
line on standard error. The gathered display and line mode leave it out, with
its `--debug` lines: a device the gate skipped shows by its failure line
(`icmp_unreachable`). The template replaced a boolean switch.

## The gathered display's header

`display.gather.header` heads each group of the gathered display
(`display.run.gather`, `display.command.gather`, `--gather`;
[`docs/OPERATIONS.md`, "Gathered output and line
mode"](OPERATIONS.md#gathered-output-and-line-mode)): the devices that
answered a command alike, under the command's block marker. The default,
`"! <targets> (<count>)"`, renders `<targets>`, the group's devices folded
into one `--tl` value, in the `target` role, bold, and `<count>`, how many,
in the `value` role, the literals in the `label` role, as the headers are
rendered:

```text
! show version
! edge[1-2],router[01-04,10] (7)
Cisco IOS XE Software, Version 17.09.04a
fake-iosxe uptime is 1 day

! router09 (1)
karvi: status=connection_error error=native_session_open_failed: dial tcp 127.0.0.9:42165: connect: connection refused
```

The header renders `<targets>`, `<count>`, and `<timestamp>`, the time the
blocks print, in the `timestamp` role. As every template, it accepts the
other placeholders of the one set every template is checked against at load,
and they render empty, or `0` for a count or an exit code, `0s` for
`<elapsed>`: `"! <target> (<count>)"` renders `!  (2)`, the name empty. In
every other template `<targets>` renders empty and `<count>` `0`. A
placeholder outside the set refuses the configuration (exit 2):

```text
display_placeholder_unsupported: unsupported display placeholder <devices> for display.gather.header at --set[1]
```

Under `display.gather.header = "[<count>] <targets>"` the groups above are
headed `[7] edge[1-2],router[01-04,10]` and `[1] router09`; an empty template
writes no header, the groups parted by the border alone. `--quiet` writes
none, nor the marker, and line mode uses none, the folded names beginning
each line instead. The marker before a block's groups is fixed, as the
collection file's marker and the `--echo` line are: `! ` (`! --sudo `,
`! session_init=` and `: `) in the `label` role, the command and a
session-init profile in the `value` role.

The header is one line however long: it is rendered once per group, never
split between elements at the terminal's width as the headers and footers
are ([Width and colors](#width-and-colors)), and the terminal wraps it. The
rendered line's limit (`display_line_too_long`) counts what the template
adds, never the folded names of its first `<targets>`: a group's names are
the job's own, however many, and the template itself is bounded at load
(`display_template_too_long`). `--border`'s dashes after a group are as wide
as its header, `dynamic-border-length` where the template is empty.

## Line mode

`display.run.prefix` and `display.command.prefix` (`--prefix`;
[`docs/OPERATIONS.md`, "Gathered output and line
mode"](OPERATIONS.md#gathered-output-and-line-mode)) write every line of the
text display as `NAME: line`, the device's name first, an empty line `NAME: `
too; gathered, the name is the group's folded names
(`edge[1-2],router[01-04,10]: line`). The form is fixed: no template sets it.
No device header, group header, ICMP gate's line, or border is written, so
`display.run.header`, `display.command.header`, `display.gather.header`,
`display.ping.header`, and the borders take no part, and `--border` and
`--noborder` are refused beside `--prefix` (`prefix_option_conflict`). The
footer and a collection's line are their templates', beginning with `!` by
default as a block's marker does, so karvi's lines stand apart from the
devices'.

The colours are by role: the name in the `target` role, bold, `: ` in the
`label` role, a failure or not-attempted line in the `error` role, bold, and
the device's output and the echo line uncoloured, as the ordinary display
leaves them. Colour follows `display.color` as every line does, and a regular
file is written plain whatever the policy, so a display redirected to a file
holds `NAME: line` and karvi's `!` lines alone.

## The recorded login's lines

`display.record.header` and `display.record.footer` name a recorded login's
transcript (`login --record`,
[`docs/LOGIN-TRANSCRIPTS.md`](LOGIN-TRANSCRIPTS.md)): the header before the
session's first line, the footer as its last, after the device's last output and
for a session that failed as well. Both default to `"!
transcript=<transcript>"`, so the two lines match; `<transcript>` is the file's
path, in the `value` role with the literals in the `label` role, as the login's
header is rendered. The templates may also use `<timestamp>`, `<target>`,
`<platform>`, and `<transport>`, and the footer `<exit-status>`, `<exit-code>`,
and `<elapsed>`. They go to standard error, on the terminal and never into the
transcript; an empty template prints no line; `--quiet` suppresses both; a
template that does not render refuses the configuration
(`config_display_template_invalid`), so no login starts and no transcript is
created.

## Borders as separators

A border separates adjacent command records. With the default `"\n"`, two
records have one blank line between them. No border follows the last record
unless the applicable `last-border` is true. In the gathered display a border
separates adjacent groups and blocks; line mode writes none.

`--border` replaces the configured border with dashes. The length is the visible
width of the displayed header (in the gathered display, the group's header);
with no header, the mode-specific `dynamic-border-length` is used. The line is
capped at terminal width.

`--noborder` suppresses configured and dynamic borders. Combining `--border`
and `--noborder` is a usage error. A configured empty string disables the
static separator without requiring a CLI option.

## Width and colors

Visible width excludes ANSI escape sequences. Overlong headers and footers are
split only between complete template elements, with a trailing artifacts
element moved to a new line first; the gathered display's group header is
never split. Only border lines may be cropped. Device output and
prompt/command echo are preserved byte-for-byte after the existing ANSI
policy.

Dark-theme defaults render target bold yellow, address bold magenta (as the
light theme does), labels and brackets blue, values white, success green, and
borders gray. Timestamp and dynamic-border roles
are independently configurable.

Colour is `display.color`'s: `auto`, the default, colours at a terminal
and not on a pipe, unless `NO_COLOR` is set and not empty (the no-color.org
convention); `always` colours a terminal and a pipe, so `| less -R` keeps
it; `never` colours nothing; and `NO_COLOR` overrides neither `always` nor
`never`. A regular file is written plain whatever the policy, so a
redirected stream or a log holds no escape. `--nocolor`, a global option
before the command word, sets `display.theme = "nocolor"` for the
invocation and so turns every colour off: `config show` names it the
value's source, `--set` outranks it, and a site that locks `display.theme`
refuses it. The rule is one, for the help, a run's display on both paths,
every warning line (`warning: …`, the whole line in the `warning` role's
colour and bold, as the `!` warning lines; a recorded login's lines at its
wrapper's terminal too), every refusal (the code and its message, in the
`error` role's colour and bold, as a device's failure line), a line a
stream drops and goes on from (`stream line 3 dropped: …`, in the `warning`
role's colour and bold), `config colors`, the watch screen (whose `--color`
sets it for the screen), `karvi-prune -h`, which reads no configuration
and colours under `auto` alone, and `karvi-audit`, which reads none either
and colours a terminal and a pipe, never a file, until `--nocolor` or
`NO_COLOR`. A refusal before the configuration is read, a mistake on the
command line or a configuration that does not load, takes the built-in
settings under the command line's own `--nocolor`.

`karvi config colors` prints the colour test:
every `display.colors.*` role under the configured theme first and then
the other, each line the key rendered as a display line renders that role
(the target and the address bold), the colour it resolves to, `default`
or `configured`, and the escape a terminal receives; a configured override
holds under both themes. Colour follows the rule above, so on a pipe the
words print alone and the escape column still says what a terminal gets;
`--set display.color=always` forces it.

`--help` follows the same rule: under
`display.color` and `display.theme`, where colour is on, the title line's tag
(`- run the fleet, gather the output`) takes the `label` colour, a
section heading is bold, an option's words take the `accent` colour, and
the action words of
the `Usage:` lines (`run` in `karvi run [target inputs]`, `daemon start`,
`setup shared` after a plain `sudo`, `<command>` in the top text) take
the `success` colour, green under both
themes and `display.colors.success` overriding; a `--config` or `--set`
before the `--help` is read; `--nocolor` or `--ansi strip` turns it off; a
configuration that does not load gives plain help. The words are never
changed, so `--help | grep` reads as before; the layout adds a blank line
around a paragraph inside a section and after an entry of three lines or
more.

## Echo

`display.command.echo`, `display.run.echo`, and `--echo` show the effective
prompt plus exact command before text output. This includes device-reported
command errors. Structured records distinguish observed and inferred prompts.
The gathered display shows no echo line: `--echo` beside `--gather` is refused
(`gather_option_conflict`), and the echo keys, or `--echo` beside a gather key
set true, show none. In line mode the echo line begins `NAME: ` as every line
does.

## Output formats

- `text`: human projection with optional header, echo, separator, and footer;
  gathered at the job's end (`display.run.gather`, [the gathered display's
  header](#the-gathered-displays-header)) or in line mode
  (`display.run.prefix`, [line mode](#line-mode)).
- `jsonl`: one compact record per line for programs.
- `json`: one indented JSON array for human inspection.

`display.json.indent` accepts 1 through 8 and affects only `json`.
