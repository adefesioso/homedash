package agent

// OmpRelease is GitHub's "latest release" alias for oh-my-pi: the same
// URL always resolves to whatever is newest, so a from-scratch install
// (this hub's first start, or a fresh remote) lands on the current
// release rather than one pinned when the hub was built. Once a binary
// exists, staying current is the omp binary's own `update` command's job
// (Agent.Reinstall on the hub, Fleet.UpdateOmp on a remote), not a
// version this hub carries.
const OmpRelease = "https://github.com/can1357/oh-my-pi/releases/latest/download/"

// LlmfitVersion is the llmfit release enrollment installs beside omp on a
// remote, and LlmfitRelease is where its tarballs and checksums live.
const (
	LlmfitVersion = "v1.1.15"
	LlmfitRelease = "https://github.com/AlexsJones/llmfit/releases/download/" + LlmfitVersion + "/"
)
