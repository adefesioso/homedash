# ui

The panel: Svelte 5 + Vite, built to static files in `dist/` by `npm run
build` and carried into the binary by `embed.go`, so there is no Node at
runtime — the two typefaces (Inter, JetBrains Mono, latin only) ride
along in the bundle rather than being fetched. Each tab in `src/` is one
file named for it — Hosts, Network, Storage, Apps, Catalog, Models,
Agents (with Terminal), Jobs, Tasks, Peers, Health, Events, Settings —
and
`App.svelte` is the shell: sign-in through `Auth.svelte`, then a rail
down the left with
the tabs in the docs' order and grouped the way the docs are (the house,
the work, the space, the hub). Beside each word the rail carries what the
tab would say first — how many machines, a red LED if one is offline, a
job that needs you — so the place to look is visible before it is read.
Each tab opens on an honest empty state (`Empty.svelte`, one line and the
one action) when there is nothing yet. `Cli.svelte` is the one page that
is not a tab: `#cli?port=…&state=…`, where a signed-in person approves
the command line that opened the browser here, as the
[cli package](../internal/cli/README.md) describes. `lib/api.js` is the
one fetch wrapper; `lib/format.js` formats bytes and ages;
`Icon.svelte` holds the rail's glyphs; `ModelPick.svelte` is the one
provider-and-model picker, used by Settings for the fleet defaults and by
a host card for its override, so a model is chosen the same way
everywhere. `Restore.svelte` is the one restore form — an export file
and its passphrase, then waiting for the hub to come back — used by
Settings and, while the hub has no account yet, by the setup page.

`app.css` is the one place a look is decided: the tokens (graphite with
an amber LED for the accent in the dark, aluminium in the light; green
and red only ever mean online and offline), the base of every control,
table, form and plate — a front-plate is `.card.plate` with a `header`
strip and a `footer` of actions, and any tab may draw one. `ui/` holds
the few pieces that are markup as well as look, so they are written once:
`Notice` (an error or a warning, with its ×), `Stat` (a value over its
label), `Fill` (a gauge bar that turns red when hot) and `Seg` (a
segmented switch). A tab styles only what is its own — the model grid,
the health checks, the settings index — and never re-declares a plate, a
gauge or an error line.

The panel holds no state the hub doesn't: every tab asks the API on open
and on a short interval (`lib/poll.js`, which pauses while the tab is
hidden and runs once when it is shown again), and shows what the machine
last reported. What only grows is asked for incrementally — a job's
lines after the last one seen, a host's metrics once a minute rather
than with every card refresh. The terminal (xterm) is loaded when the
first session is opened, not with the page. A
viewer sees every tab read-only; the API refuses the writes, the panel
merely hides the buttons.

Every tab fits a phone (390px) without the page itself scrolling
sideways — a table scrolls in its own box instead; Escape dismisses an
inline card (New remote, an edit) the way it would a `<dialog>`; a tab's
hash is matched loosely (`#Hosts`, `#hosts/x`) rather than only its exact
lowercase id.
