// In-flight model pulls, kept at module scope rather than inside
// Models.svelte: component state resets on remount, so switching to
// another tab mid-pull used to make the progress row vanish and nothing
// on Models said a pull was still running (M-2). This lives for as long
// as the panel tab does, so coming back to Models still shows it.
//
// A ".svelte.js" module, not plain ".js": Svelte 5 only allows $state at
// module scope in a file with this extension.
export const pulls = $state({}); // "host/model" -> last progress line, or a terminal error
