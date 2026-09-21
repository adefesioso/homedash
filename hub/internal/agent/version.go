package agent

// OmpVersion is the omp release this hub was tested against. The hub drives
// omp over its own protocol, so the version is the hub's to choose: it is
// fetched on first start, updated with the hub, and never by hand.
const OmpVersion = "v18.1.21"

// ompRelease is where the pinned binaries and their checksums come from.
const ompRelease = "https://github.com/can1357/oh-my-pi/releases/download/" + OmpVersion + "/"

// LlmfitVersion is the llmfit release enrollment installs beside omp on a
// remote, and LlmfitRelease is where its tarballs and checksums live.
const (
	LlmfitVersion = "v1.1.15"
	LlmfitRelease = "https://github.com/AlexsJones/llmfit/releases/download/" + LlmfitVersion + "/"
)
