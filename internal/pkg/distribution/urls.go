package distribution

const gitHub = "https://github.com/"

// ReleasesURL is the GitHub API's list of the repository's newest releases.
func (d Distribution) ReleasesURL() string {
	return "https://api.github.com/repos/" + d.Repository + "/releases?per_page=30"
}

// DownloadBase is where GoReleaser publishes a release's files; the tag and
// the file name follow.
func (d Distribution) DownloadBase() string {
	return gitHub + d.Repository + "/releases/download/"
}

// WorkflowIdentityPrefix is the certificate identity of the repository's
// release workflow run for a tag; the tag itself is appended, so a signature
// made for one release does not pass for another.
func (d Distribution) WorkflowIdentityPrefix() string {
	return gitHub + d.Repository + "/.github/workflows/release.yml@refs/tags/"
}
