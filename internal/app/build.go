package app

import (
	"fmt"
	"strings"

	"github.com/gsoultan/metis/internal/pkg/distribution"
)

// UseBuild has the program that embeds the engine name its own build: the
// version its liveness probe, traces and Upgrades page report, and the
// distribution whose releases it is compared with and upgraded from. See
// engine.WithBuild.
//
// The version is the process's, as the link-time stamp is: one engine runs in
// a process. An empty version keeps the stamp, so a program built without
// one still says "dev" rather than nothing.
func (a *App) UseBuild(buildVersion string, d distribution.Distribution) {
	if err := d.Validate(); err != nil {
		a.buildErr = fmt.Errorf("the embedding program's build: %w", err)
		return
	}
	a.distribution = d
	if buildVersion = strings.TrimSpace(buildVersion); buildVersion != "" {
		version = buildVersion
	}
}
