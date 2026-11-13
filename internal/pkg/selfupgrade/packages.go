package selfupgrade

import (
	"fmt"
	"strings"
)

// Format is the package manager that owns /usr/bin/metis on this host.
type Format string

const (
	FormatDeb  Format = "deb"
	FormatRPM  Format = "rpm"
	FormatAPK  Format = "apk"
	FormatArch Format = "archlinux"
)

// Probe is what DetectFormat looks at: whether a package manager knows the
// metis package. Asking the package database, not which tools are installed,
// because a Debian host can have rpm installed too.
type Probe func(f Format) bool

// DetectFormat names the package manager that installed metis, or fails: a
// binary nobody's package manager owns is replaced by whoever placed it.
func DetectFormat(installed Probe) (Format, error) {
	for _, f := range []Format{FormatDeb, FormatRPM, FormatAPK, FormatArch} {
		if installed(f) {
			return f, nil
		}
	}
	return "", fmt.Errorf("no package manager on this host installed metis, so this is not a package install; replace the binary the way it was placed")
}

// PackageName is the file GoReleaser publishes for a format, release and
// architecture (Go's names: amd64, arm64).
func PackageName(f Format, tag, goarch string) (string, error) {
	version := strings.TrimPrefix(tag, "v")
	machine := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[goarch]
	if machine == "" {
		return "", fmt.Errorf("no package is published for %s", goarch)
	}
	switch f {
	case FormatDeb:
		return fmt.Sprintf("metis_%s_%s.deb", version, goarch), nil
	case FormatRPM:
		return fmt.Sprintf("metis-%s-1.%s.rpm", version, machine), nil
	case FormatAPK:
		return fmt.Sprintf("metis_%s_%s.apk", version, machine), nil
	case FormatArch:
		return fmt.Sprintf("metis-%s-1-%s.pkg.tar.zst", version, machine), nil
	}
	return "", fmt.Errorf("unknown package format %q", f)
}

// InstallCommand installs a verified package file with its package manager,
// so the package database stays the truth about what is installed. Downgrade
// flags are included because a rollback installs the older release.
func InstallCommand(f Format, path string) (string, []string, error) {
	switch f {
	case FormatDeb:
		return "dpkg", []string{"-i", path}, nil
	case FormatRPM:
		return "rpm", []string{"-U", "--oldpackage", path}, nil
	case FormatAPK:
		// Our apk files are not signed with an Alpine key; their checksum was
		// verified against the signed checksums.txt before this runs.
		return "apk", []string{"add", "--allow-untrusted", path}, nil
	case FormatArch:
		return "pacman", []string{"-U", "--noconfirm", path}, nil
	}
	return "", nil, fmt.Errorf("unknown package format %q", f)
}

// QueryCommand asks a package manager whether metis is installed.
func QueryCommand(f Format) (string, []string) {
	switch f {
	case FormatDeb:
		return "dpkg-query", []string{"-W", "metis"}
	case FormatRPM:
		return "rpm", []string{"-q", "metis"}
	case FormatAPK:
		return "apk", []string{"info", "-e", "metis"}
	default:
		return "pacman", []string{"-Q", "metis"}
	}
}
