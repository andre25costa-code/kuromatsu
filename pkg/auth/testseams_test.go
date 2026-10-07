package auth

// Test seams: helpers that only tests use. They live here, outside the
// production binary, after the dead-code cleanup moved them out of the
// package sources.

func setAnthropicUsageURL(url string) { anthropicUsageURL = url }
