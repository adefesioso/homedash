# The panel on a phone

Under 860px the tabs are a strip
pinned to the top that scrolls the active tab into view; under 560px a
`table.stack` lays each row out as a wrapped card with its detail row
full width, and the Models matrix pins its name column while the rest
scrolls; on a coarse pointer every control is ≥38px tall and text inputs
are 16px so iOS does not zoom on focus; `public/manifest.webmanifest`
and the icons beside it let the panel be added to a home screen. Every
tab fits 390px without the page itself scrolling sideways — a table
scrolls in its own box instead; Escape dismisses an inline card (New
remote, an edit) the way it would a `<dialog>`; a tab's hash is matched
loosely (`#Hosts`, `#hosts/x`) rather than only its exact lowercase id.
