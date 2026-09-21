# internal/notify

The notification target. `Send(kind, subject, message)` queues one
transition for a goroutine that POSTs it to whatever `notify.target`
names at that moment: an ntfy topic URL gets a title, a body and a tag
that says trouble or its end; any other URL gets the same as JSON.
Best-effort — a full queue drops rather than blocks, a failed POST is
logged — so nothing that reports a transition ever waits on the network.
There is no per-event picker because every event that reaches this
package is already a transition into or out of trouble.
