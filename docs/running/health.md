# Is the hub well

The hub is the cheapest machine running the longest hours, so the panel
gives it what it gives every remote: a page that shows what the machine
reports about itself, never what anyone asked for. **Health**, under Hub,
is that page. The top is the box — cores, load, memory, uptime, and the
disk the state file sits on with how much of it is left. Under it is one
line per thing the hub has to keep running for the lab to work: the
agent and its credential vault, how old your last export is, the hosts heartbeating in, the schedule, the space. Each line is
green, amber or red with the reason written beside it, the page's LED in
the rail is the worst of them, and the disk line turns red at the same
fullness threshold a remote's does (`notify.disk_percent`, under
Settings › Agents). The same facts are `GET /api/hub/health` for the CLI
and for anything watching the hub from outside.

Nothing on the page is a control. What needs doing is done where it
lives — Settings for an export, Hosts for a machine, Tasks for a
failing schedule — and the page only says which.
