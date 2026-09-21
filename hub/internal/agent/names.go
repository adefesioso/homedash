package agent

import (
	"fmt"
	"math/rand/v2"
)

// A window's name is a passphrase the hub draws, adjective-noun, rather
// than something a person types: the panel asked for a name on every open
// and nobody had one to give. Two short lists make a few thousand pairs,
// plenty for the handful of windows live at once (Open retries on a
// clash) and short enough to say out loud.
var (
	adjectives = []string{
		"amber", "bold", "bounty", "brave", "brisk", "calm", "clever", "cosmic", "crisp", "daring",
		"dusty", "eager", "early", "fancy", "fuzzy", "gentle", "giddy", "golden", "happy", "hasty",
		"humble", "jolly", "keen", "kind", "lively", "lucky", "merry", "mighty", "misty", "noble",
		"nimble", "plucky", "proud", "quick", "quiet", "rapid", "rosy", "rusty", "shiny", "silent",
		"sleepy", "smooth", "snowy", "spicy", "sturdy", "sunny", "swift", "tidy", "vivid", "witty",
	}
	nouns = []string{
		"badger", "beaver", "bison", "camel", "cobra", "condor", "coyote", "crane", "dingo", "donkey",
		"eagle", "falcon", "ferret", "gecko", "gibbon", "heron", "hyena", "ibis", "jackal", "koala",
		"lemur", "llama", "lynx", "magpie", "marmot", "moose", "narwhal", "newt", "ocelot", "osprey",
		"otter", "panda", "parrot", "pelican", "puffin", "quokka", "rabbit", "raven", "robin", "salmon",
		"shrew", "sloth", "sparrow", "stork", "tapir", "toucan", "turtle", "walrus", "wombat", "yak",
	}
)

// drawName is one adjective-noun pair.
func drawName() string {
	return fmt.Sprintf("%s-%s", adjectives[rand.IntN(len(adjectives))], nouns[rand.IntN(len(nouns))])
}
