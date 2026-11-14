package verbs

// DogfoodReleaseGuidance accompanies lease grants, deployments, and holder
// notices, and is also included in the MCP tool descriptions.
const DogfoodReleaseGuidance = "Don't hold dogfood for a user who is away. " +
	"Don't hold dogfood while fixing a problem you found: call iris_dogfood_release, fix it, then get back in line with a new iris_dogfood_claim. " +
	"If the user is away and Iris asks for dogfood back or says someone is waiting, call iris_dogfood_release right away."
