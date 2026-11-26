package distribution

// Where a Linux package of the distribution installs itself. Each package's
// .goreleaser.yaml must agree: see deploy/linux.

// Binary is the program the package installs.
func (d Distribution) Binary() string { return "/usr/bin/" + d.Name }

// SystemdUnit is the service's unit on a systemd host.
func (d Distribution) SystemdUnit() string { return "/usr/lib/systemd/system/" + d.Name + ".service" }

// OpenRCScript is the service's script on an OpenRC host (Alpine).
func (d Distribution) OpenRCScript() string { return "/etc/init.d/" + d.Name }

// EnvFile is the service's environment file.
func (d Distribution) EnvFile() string { return "/etc/" + d.Name + "/" + d.Name + ".env" }

// StateDir is the service's working directory, which it owns.
func (d Distribution) StateDir() string { return "/var/lib/" + d.Name }

// UpgradeTriggerEnabled exists while the package's <Name>-upgrade.path is
// enabled, the unit that runs an upgrade the Upgrades page asked for.
func (d Distribution) UpgradeTriggerEnabled() string {
	return "/etc/systemd/system/paths.target.wants/" + d.Name + "-upgrade.path"
}
